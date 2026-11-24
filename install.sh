#!/bin/sh
# Installs slash for you (no sudo): the program goes to ~/.local/bin and is
# turned on for your shell. Install, or update to the newest version:
#
#   curl -fsSL https://raw.githubusercontent.com/tobias-martens/slash-shell/main/install.sh | sh
#
# Remove slash again, leaving everything exactly as it was before:
#
#   curl -fsSL https://raw.githubusercontent.com/tobias-martens/slash-shell/main/install.sh | sh -s -- --uninstall
#
# SLASH_VERSION=v1.0.0 installs that version instead of the newest one.
# SLASH_BINARY=/path/to/slash installs that file instead of downloading
# (used by the tests).
#
# Everything runs inside main, at the very end: if the download of this
# script breaks off, nothing runs.

set -eu

repo=tobias-martens/slash-shell
bindir=$HOME/.local/bin
statedir=${XDG_STATE_HOME:-$HOME/.local/state}/slash
cachedir=${XDG_CACHE_HOME:-$HOME/.cache}
# Folders that may not exist yet. The ones the install creates (slash's own
# state and cache folders included) are removed on uninstall, if empty.
# Written next to the program, as the state folder goes with `slash disable`.
dirs="$HOME/.local $bindir ${statedir%/slash} $cachedir"
dirsfile=$bindir/.slash-created-dirs

say() { printf '%s\n' "$*"; }
die() { printf 'slash install: %s\n' "$*" >&2; exit 1; }

download() { # URL FILE
    if command -v curl >/dev/null; then
        curl -fsSL --proto '=https' -o "$2" "$1"
    elif command -v wget >/dev/null; then
        wget -q -O "$2" "$1"
    else
        die "I need curl or wget to download slash."
    fi
}

arch() {
    case $(uname -m) in
        x86_64 | amd64) echo amd64 ;;
        aarch64 | arm64) echo arm64 ;;
        armv7* | armv6*) echo arm ;;
        i?86) echo 386 ;;
        *) die "there is no slash for $(uname -m) yet." ;;
    esac
}

install_slash() {
    [ "$(uname -s)" = Linux ] || die "slash is for Linux."
    if [ "$(id -u)" = 0 ] && [ -n "${SUDO_USER:-}" ]; then
        die "run this without sudo: slash is installed for your user only."
    fi

    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    if [ -n "${SLASH_BINARY:-}" ]; then
        cp "$SLASH_BINARY" "$tmp/slash"
    else
        a=$(arch)
        if [ -n "${SLASH_VERSION:-}" ]; then
            url=https://github.com/$repo/releases/download/$SLASH_VERSION
        else
            url=https://github.com/$repo/releases/latest/download
        fi
        say "Downloading slash for $a ..."
        download "$url/slash-linux-$a" "$tmp/slash-linux-$a" || die "download failed: $url/slash-linux-$a"
        download "$url/SHA256SUMS" "$tmp/SHA256SUMS" || die "download failed: $url/SHA256SUMS"
        (cd "$tmp" && grep " slash-linux-$a\$" SHA256SUMS | sha256sum -c - >/dev/null 2>&1) ||
            die "the download is damaged (checksum mismatch); nothing was installed."
        mv "$tmp/slash-linux-$a" "$tmp/slash"
    fi
    chmod 755 "$tmp/slash"
    "$tmp/slash" version >/dev/null 2>&1 || die "the downloaded program doesn't run on this system."

    other=$(command -v slash 2>/dev/null || true)
    if [ -n "$other" ] && [ "$other" != "$bindir/slash" ]; then
        say "Note: there is another slash at $other; that one comes first in PATH."
    fi

    # Remember which folders didn't exist before (kept from an earlier install).
    created=""
    [ -f "$dirsfile" ] && created=$(cat "$dirsfile")
    for d in $dirs; do
        [ -e "$d" ] || created="$created $d"
    done
    mkdir -p "$bindir"
    mv "$tmp/slash" "$bindir/slash"
    [ -n "${created# }" ] && printf '%s\n' $created | sort -u >"$dirsfile"
    say "Installed $("$bindir/slash" version) in $bindir."
    say ""

    if "$bindir/slash" enable </dev/null; then
        login=$(basename "${SHELL:-}")
        for sh in bash zsh fish; do
            [ "$sh" != "$login" ] && command -v "$sh" >/dev/null &&
                say "Using $sh too? Turn slash on there with: slash enable $sh"
        done
    else
        say "slash works in bash, zsh and fish. Turn it on with, for example: $bindir/slash enable bash"
    fi
    say "Remove slash again: run this installer with --uninstall"
    return 0
}

uninstall_slash() {
    if [ ! -x "$bindir/slash" ]; then
        if other=$(command -v slash 2>/dev/null); then
            die "slash at $other wasn't installed by this script. Turn it off with: slash disable, then remove it the way you installed it."
        fi
        say "slash isn't installed."
        return 0
    fi
    created=""
    [ -f "$dirsfile" ] && created=$(cat "$dirsfile")
    "$bindir/slash" disable </dev/null | sed '/^To remove the program itself/d'
    rm -f "$bindir/slash" "$dirsfile"
    # Innermost first; rmdir only removes empty folders.
    for d in $(printf '%s\n' $created | sort -r); do
        rmdir "$d" 2>/dev/null || true
    done
    say "slash is removed. Terminals that are open keep working as before."
}

main() {
    case ${1:-} in
        "") install_slash ;;
        --uninstall) uninstall_slash ;;
        *) die "unknown option $1 (the only one is --uninstall)" ;;
    esac
}

main "$@"
