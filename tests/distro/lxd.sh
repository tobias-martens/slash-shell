#!/bin/sh
# Runs tests/distro/check.sh in fresh LXD containers, one per distribution.
#
#   tests/distro/lxd.sh [debian ubuntu fedora arch opensuse alpine]
#
# Needs LXD (lxc) and Go. Containers are ephemeral: stopping deletes them.
# Logs go to $LOGDIR (default: a new temporary folder).
set -u
cd "$(dirname "$0")/../.."

image() {
    case $1 in
        debian) echo images:debian/13 ;;
        ubuntu) echo ubuntu:24.04 ;;
        fedora) echo images:fedora/43 ;;
        arch) echo images:archlinux ;;
        opensuse) echo images:opensuse/tumbleweed ;;
        alpine) echo images:alpine/edge ;;
        *) return 1 ;;
    esac
}

distros=${*:-debian ubuntu fedora arch opensuse alpine}
LOGDIR=${LOGDIR:-$(mktemp -d)}
bin=$LOGDIR/slash
CGO_ENABLED=0 go build -o "$bin" ./cmd/slash || exit 1
echo "logs: $LOGDIR"

results=""
for d in $distros; do
    img=$(image "$d") || { echo "unknown distribution $d"; exit 2; }
    name=slash-test-$d
    out=$LOGDIR/$d.log
    printf '%-9s ' "$d"
    lxc delete --force "$name" >/dev/null 2>&1
    if ! timeout 300 lxc launch "$img" "$name" --ephemeral >"$out" 2>&1; then
        echo "could not start ($out)"; results="$results $d:start-failed"; continue
    fi
    # Wait for an address and a default route.
    i=0
    until lxc exec "$name" -- sh -c 'ip -4 route 2>/dev/null | grep -q default' 2>/dev/null; do
        i=$((i + 1)); [ $i -gt 60 ] && break; sleep 1
    done
    lxc file push "$bin" "$name/usr/local/bin/slash" --mode 0755 >>"$out" 2>&1
    lxc file push -r tests "$name/root/" >>"$out" 2>&1

    status=ok
    for phase in prepare online offline; do
        echo "== $phase" >>"$out"
        if ! lxc exec "$name" -- sh /root/tests/distro/check.sh "$phase" >>"$out" 2>&1; then
            status="failed-in-$phase"
            [ $phase = prepare ] && break
        fi
    done
    lxc stop --force "$name" >/dev/null 2>&1
    echo "$status ($(grep -c '^ok' "$out") ok, $(grep -c '^FAIL' "$out") failed)"
    results="$results $d:$status"
done

case $results in
    *failed*) exit 1 ;;
esac
