#!/bin/sh
# Runs tests/distro/check.sh in a fresh Docker container (used by CI).
#
#   tests/distro/docker.sh IMAGE SLASH_BINARY
#
# NET_ADMIN lets check.sh switch the container's network off for the
# offline phase. Docker containers run no systemd, so service checks take
# the "no systemd" path there (LXD containers, see lxd.sh, cover systemd).
set -eu
cd "$(dirname "$0")/../.."
image=$1
bin=$(realpath "$2")
docker run --rm --cap-add=NET_ADMIN \
    -v "$PWD/tests:/root/tests:ro" -v "$bin:/usr/local/bin/slash:ro" \
    "$image" sh -c '
        c=/root/tests/distro/check.sh
        sh $c prepare || exit 1
        sh $c online; a=$?
        sh $c offline; b=$?
        [ $a = 0 ] && [ $b = 0 ]'
