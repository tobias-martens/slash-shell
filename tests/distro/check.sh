#!/bin/sh
# Checks slash on a real distribution. Runs inside a fresh container as root,
# with slash at /usr/local/bin/slash and the tests folder at /root/tests.
#
#   check.sh prepare   install what the tests need (network on)
#   check.sh online    package round trip with the real package manager
#   check.sh offline   network off: search, about, files, shells
#
# POSIX sh: it also runs on Alpine's busybox.
set -u
export LC_ALL=C
SLASH=/usr/local/bin/slash
PKG=tree # small, and packaged as "tree" everywhere
failed=0
log=/tmp/check.out

. /etc/os-release
case "$ID" in
    debian | ubuntu) pm=apt ;;
    fedora) pm=dnf ;;
    arch | archlinux) pm=pacman ;;
    opensuse*) pm=zypper ;;
    alpine) pm=apk ;;
    *) echo "unknown distribution $ID"; exit 2 ;;
esac

pass() { printf 'ok    %s\n' "$1"; }
fail() {
    printf 'FAIL  %s\n' "$1"
    sed 's/^/      | /' "$log" | head -n 30
    failed=1
}
# check DESCRIPTION COMMAND… : the command must succeed.
check() {
    d=$1; shift
    if "$@" >"$log" 2>&1; then pass "$d"; else fail "$d"; fi
}
# expect DESCRIPTION TEXT COMMAND… : the command's output must contain TEXT.
expect() {
    d=$1 text=$2; shift 2
    "$@" >"$log" 2>&1
    if grep -qF -- "$text" "$log"; then pass "$d"; else fail "$d (want: $text)"; fi
}
# same LINE WANT : slash rewrites LINE to exactly WANT.
same() {
    got=$($SLASH resolve --line "$1" 2>&1)
    if [ "$got" = "$2" ]; then pass "resolve: $1"; else echo "$got" >"$log"; fail "resolve: $1 (want: $2)"; fi
}

prepare() {
    case $pm in
        apt) apt-get update -q && DEBIAN_FRONTEND=noninteractive apt-get install -y -q python3 zsh fish iproute2 procps zip unzip ;;
        dnf) dnf -y -q install python3 zsh fish iproute procps-ng findutils zip unzip ;;
        pacman) pacman -Syu --noconfirm --needed python zsh fish iproute2 procps-ng zip unzip ;;
        zypper) zypper -n -q refresh && zypper -n -q install python3 zsh fish iproute2 procps findutils util-linux zip unzip ;;
        apk) apk update -q && apk add -q python3 bash zsh fish iproute2 procps coreutils findutils zip unzip ;;
    esac
}

# answered COMMAND: runs it answering "y" to its questions. A fixed number
# of answers: an endless `yes` into a terminal filled the container's
# memory. zypper won't read answers from a pipe, so it gets a terminal, and
# it drops typed-ahead input before asking, so the answers come slowly.
answered() {
    if [ $pm = zypper ]; then
        (for i in 1 2 3 4 5; do sleep 8; echo y; done) | script -qec "$*" /dev/null
    else
        printf 'y\ny\ny\ny\ny\n' | sh -c "$*"
    fi
}

# What `update` must be on this system (as root: no sudo).
expected_update() {
    case $pm in
        apt) u="apt update && apt upgrade" ;;
        dnf) u="dnf upgrade" ;;
        pacman) u="pacman -Syu" ;;
        zypper) if [ "$ID" = opensuse-tumbleweed ]; then u="zypper dist-upgrade"; else u="zypper update"; fi ;;
        apk) u="apk update && apk upgrade" ;;
    esac
    command -v flatpak >/dev/null && u="$u; flatpak update"
    command -v snap >/dev/null && u="$u; snap refresh"
    echo "$u  #slash: update"
}

online() {
    expect "doctor finds $pm" "Sources:   $pm (system)" $SLASH doctor
    same update "$(expected_update)"
    check "coreutils install is left alone" sh -c "! $SLASH resolve --line 'install -m 755 a b'"

    # The real round trip. Installed or not is read from the package
    # database (busybox may provide its own "tree" command).
    answered "$SLASH install $PKG" >"$log" 2>&1
    if $SLASH about $PKG | grep -q " installed "; then pass "install $PKG"; else fail "install $PKG"; fi
    expect "about $PKG: installed" "installed" $SLASH about $PKG
    expect "search $PKG: marked installed" "[installed]" $SLASH search $PKG in packages
    answered "$SLASH remove $PKG" >"$log" 2>&1
    if $SLASH about $PKG | grep -q " available "; then pass "remove $PKG"; else fail "remove $PKG"; fi
    if [ $pm = apk ]; then
        expect "clean up: says it can't yet" "don't know how to clean up" $SLASH clean up
    else
        check "clean up" answered "$SLASH clean up"
    fi
}

offline() {
    ip link set eth0 down 2>/dev/null
    if ip -4 route | grep -q default; then echo "network still on"; exit 2; fi
    pass "network is off"

    # Runs after "clean up": package lists must have survived it.
    expect "search $PKG offline" "$PKG" $SLASH search $PKG in packages
    expect "about $PKG offline" "available" $SLASH about $PKG
    expect "search editor: lists packages" "($pm" $SLASH search editor
    expect "about nosuch: says so" "No package called" $SLASH about no-such-package-xyz

    mkdir -p /root/Documents && : >/root/Documents/holiday-2024.txt
    expect "search files with find" "~/Documents/holiday-2024.txt" sh -c "cd /root && $SLASH search holiday"
    expect "search *.txt" "holiday-2024.txt" sh -c "cd /root && $SLASH search '*.txt' in files"

    check "show disk space runs" sh -c "eval \"\$($SLASH resolve --line 'show disk space')\""
    check "show memory runs" sh -c "eval \"\$($SLASH resolve --line 'show memory')\""

    cd /tmp && rm -rf packtest && mkdir -p packtest/Photos && echo hi >packtest/Photos/a.txt && cd packtest
    check "pack and unpack (zip)" sh -c "$SLASH pack Photos && $SLASH unpack Photos.zip into Zipped && test -f Zipped/Photos/a.txt"
    check "pack and unpack (tar.gz)" sh -c "$SLASH pack Photos as tar.gz && $SLASH unpack Photos.tar.gz into Copy && test -f Copy/Photos/a.txt"
    # Fallbacks: with only python3 (or bsdtar) slash still makes a zip. The
    # command is chosen with a PATH holding just that program, then run.
    mkdir -p /tmp/only && ln -sf "$(command -v python3)" /tmp/only/python3
    check "pack with only python3: a zip" sh -c "eval \"\$(PATH=/tmp/only $SLASH resolve --line 'pack Photos as Py.zip')\" && unzip -l Py.zip | grep -q Photos/a.txt"
    if command -v bsdtar >/dev/null; then
        rm -f /tmp/only/* && ln -sf "$(command -v bsdtar)" /tmp/only/bsdtar
        check "pack and unpack with only bsdtar" sh -c "eval \"\$(PATH=/tmp/only $SLASH resolve --line 'pack Photos as Bsd.zip')\" && unzip -l Bsd.zip | grep -q Photos/a.txt && eval \"\$(PATH=/tmp/only $SLASH resolve --line 'unpack Bsd.zip into B')\" && test -f B/Photos/a.txt"
    fi
    mkdir -p /tmp/targz && ln -sf "$(command -v tar)" /tmp/targz/tar && ln -sf "$(command -v gzip)" /tmp/targz/gzip
    expect "pack with nothing for zip: a tar.gz" "tar -czf Copy.tar.gz Copy" env PATH=/tmp/targz $SLASH resolve --line 'pack Copy'
    check "unpack keeps existing files" sh -c "echo mine >Photos/a.txt; ! $SLASH unpack Photos.tar.gz && grep -q mine Photos/a.txt"
    # delete: into the home trash (/tmp may be another drive). With gio or
    # trash-put that tool does it, else slash itself; same place either way.
    echo x >"/root/old notes.txt"
    { command -v gio || command -v trash-put; } >/dev/null || pass "(no gio or trash-put: slash's own trash)"
    check "delete: to the trash" sh -c "cd /root && $SLASH delete 'old notes.txt' && test ! -e 'old notes.txt' && test -f '.local/share/Trash/files/old notes.txt' && grep -q '^Path=/root/old%20notes.txt$' '.local/share/Trash/info/old notes.txt.trashinfo'"
    cd /

    if [ -d /run/systemd/system ]; then
        expect "stop: lists services" "Services:" $SLASH stop
        expect "restart unknown service" "no service called" $SLASH restart no-such-service-xyz
    else
        expect "start: needs systemd" "only know systemd" $SLASH start sshd
    fi

    check "install script: install, use, uninstall" env SLASH_BIN=$SLASH sh /root/tests/install_test.sh

    if command -v python3 >/dev/null; then
        check "shell tests (bash, zsh, fish)" env SLASH_BIN=$SLASH python3 -I /root/tests/shell_test.py
    else
        fail "python3 missing"
    fi
}

case ${1:-} in
    prepare) prepare >"$log" 2>&1 || { fail "prepare"; exit 1; } ;;
    online) online ;;
    offline) offline ;;
    *) echo "usage: check.sh prepare|online|offline"; exit 2 ;;
esac
exit $failed
