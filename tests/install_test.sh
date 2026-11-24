#!/bin/sh
# Tests install.sh without network (SLASH_BINARY): install, use in a new
# shell, uninstall, and the home folder must be exactly as before.
#
#   SLASH_BIN=./slash tests/install_test.sh
#
# Each case gets its own temporary home. POSIX sh (runs on Alpine too).
set -u
cd "$(dirname "$0")/.."
installer=$PWD/install.sh
bin=$(realpath "${SLASH_BIN:-./slash}")
failed=0
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT

pass() { printf 'ok    %s\n' "$1"; }
fail() { printf 'FAIL  %s\n' "$1"; [ -f "$root/out" ] && sed 's/^/      | /' "$root/out" | head -n 20; failed=1; }

# Everything in a home folder: names, then each file's checksum and mode.
snapshot() {
    (cd "$1" && find . -print | LC_ALL=C sort && find . -type f -exec cksum {} + | LC_ALL=C sort &&
        find . -exec stat -c '%a %n' {} + | LC_ALL=C sort)
}

# run HOME SHELL PATH COMMAND… : runs a command as that user would.
run() {
    _h=$1 _sh=$2 _path=$3; shift 3
    env -i HOME="$_h" SHELL="$_sh" PATH="$_path" TERM=dumb LANG=C.UTF-8 HISTFILE=/dev/null "$@"
}

# What a new interactive shell sees: the slash command and the update function.
new_shell() { # HOME SHELL PATH
    case $(basename "$2") in
        bash) run "$1" "$2" "$3" bash -ic 'command -v slash; type -t update' ;;
        zsh) run "$1" "$2" "$3" zsh -ic 'command -v slash; whence -w update' ;;
        fish) run "$1" "$2" "$3" fish -ic 'command -v slash; functions -q update; and echo update: function' ;;
    esac 2>/dev/null
}

# Removes what the test's own shells wrote, so only slash's traces count.
forget_shell_files() {
    rm -f "$1/.config/fish/fish_variables" "$1/.zcompdump"*
    rm -rf "$1/.local/share/fish" "$1/.cache/fish"
    rmdir "$1/.local/share" 2>/dev/null || true
}

# case NAME SHELL PATHMODE SETUP : PATHMODE is "add" (~/.local/bin not on
# PATH) or "has" (already on PATH). SETUP is run in the new home first.
case_() {
    name=$1 sh=$2 pathmode=$3 setup=$4
    if ! command -v "$sh" >/dev/null; then
        echo "skip  $name ($sh not installed)"
        return
    fi
    h=$root/$name
    mkdir -p "$h" && (cd "$h" && eval "$setup")
    before=$(snapshot "$h")
    path=/usr/bin:/bin # not /usr/local/bin: the distro tests put a slash there
    [ "$pathmode" = has ] && path=$path:$h/.local/bin
    shpath=$(command -v "$sh")

    run "$h" "$shpath" "$path" SLASH_BINARY="$bin" sh "$installer" >"$root/out" 2>&1 &&
        run "$h" "$shpath" "$path" SLASH_BINARY="$bin" sh "$installer" >>"$root/out" 2>&1 &&
        grep -q "already enabled for $sh" "$root/out" &&
        [ -x "$h/.local/bin/slash" ] &&
        pass "$name: install, twice" || fail "$name: install, twice"

    new_shell "$h" "$shpath" "$path" >"$root/out"
    grep -q "^$h/.local/bin/slash\$" "$root/out" && grep -q 'update: function\|^function$' "$root/out" &&
        pass "$name: a new $sh finds slash" || fail "$name: a new $sh finds slash"

    if [ "$pathmode" = add ]; then
        rc=$(grep -l 'slash init' "$h/.bashrc" "$h/.zshrc" "$h/.config/fish/conf.d/slash.fish" 2>/dev/null)
        grep -q '\.local/bin' $rc && pass "$name: PATH line added" || fail "$name: PATH line added"
    fi

    forget_shell_files "$h"
    run "$h" "$shpath" "$path" sh "$installer" --uninstall >"$root/out" 2>&1 &&
        grep -q "slash is removed" "$root/out" && pass "$name: uninstall" || fail "$name: uninstall"
    if [ "$(snapshot "$h")" = "$before" ]; then
        pass "$name: home folder exactly as before"
    else
        printf '%s\n' "$before" >"$root/before"
        snapshot "$h" | diff "$root/before" - >"$root/out"
        fail "$name: home folder exactly as before"
    fi
}

case_ bash-rc bash add "printf 'alias ll=\"ls -l\"' >.bashrc; chmod 600 .bashrc; mkdir -p .local/bin"
case_ zsh-empty-home zsh add ":"
case_ fish-on-path fish has "mkdir -p .config/fish .local/bin .cache; echo 'set -g x 1' >.config/fish/config.fish"

# Uninstall with nothing installed.
mkdir -p "$root/none"
run "$root/none" /bin/sh /usr/bin:/bin sh "$installer" --uninstall >"$root/out" 2>&1 &&
    grep -q "isn't installed" "$root/out" && pass "uninstall when not installed" || fail "uninstall when not installed"

exit $failed
