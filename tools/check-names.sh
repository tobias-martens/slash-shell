#!/usr/bin/env bash
# Usage: check-names.sh <idx-dir> name...
# Reports which distro packages ship an executable with any of the given names
# (in bin/sbin/games dirs). Downloads public file indexes into <idx-dir>.
set -u
D=$1; shift
mkdir -p "$D"; cd "$D" || exit 1
NAMES="$*"
F=https://dl.fedoraproject.org/pub/fedora/linux/development/rawhide/Everything/x86_64/os
S=http://download.opensuse.org/tumbleweed/repo/oss
dl() { [ -s "$1" ] || curl -sfL -o "$1" "$2" || echo "FAIL $1" >&2; }
fl() { curl -sf "$1/repodata/repomd.xml" | grep -o 'href="[^"]*filelists\.xml\.zst"' | head -1 | sed 's/href="//;s/"$//'; }
for c in main contrib non-free; do dl debian-$c.gz http://deb.debian.org/debian/dists/sid/$c/Contents-amd64.gz & done
dl debian-all.gz http://deb.debian.org/debian/dists/sid/main/Contents-all.gz &
dl ubuntu.gz http://archive.ubuntu.com/ubuntu/dists/resolute/Contents-amd64.gz &
dl fedora.zst "$F/$(fl $F)" &
dl suse.zst "$S/$(fl $S)" &
dl arch-core.tgz https://geo.mirror.pkgbuild.com/core/os/x86_64/core.files.tar.gz &
dl arch-extra.tgz https://geo.mirror.pkgbuild.com/extra/os/x86_64/extra.files.tar.gz &
wait
ALT=$(echo $NAMES | tr ' ' '|')
RE="^/?(usr/)?(s?bin|games)/($ALT)\$"
{
  for f in debian-main debian-contrib debian-non-free debian-all; do
    zcat $f.gz | awk -v re="$RE" '$1 ~ re {n=$1; sub(/.*\//,"",n); print n, "debian", $NF}'; done
  zcat ubuntu.gz | awk -v re="$RE" '$1 ~ re {n=$1; sub(/.*\//,"",n); print n, "ubuntu", $NF}'
  for f in fedora suse; do
    zstd -dc $f.zst | awk -v re="$RE" -v d=$f '/<package /{match($0,/name="[^"]*"/); p=substr($0,RSTART+6,RLENGTH-7)}
      /<file/{s=$0; sub(/.*<file[^>]*>/,"",s); sub(/<\/file>.*/,"",s); if (s ~ re) {n=s; sub(/.*\//,"",n); print n, d, p}}'; done
  for f in arch-core arch-extra; do
    [ -d $f ] || { mkdir -p $f && tar -xzf $f.tgz -C $f; }
    for p in $f/*/files; do grep -Ex "${RE#^/?}" "$p" | sed "s#.*/##;s#\$# arch $(basename $(dirname $p))#"; done; done
} | sort -u
for n in $NAMES; do
  curl -sf "https://pkgs.alpinelinux.org/contents?file=$n&path=&name=&branch=edge&repo=&arch=x86_64" \
    | grep -qE "/(usr/)?s?bin/$n<" && echo "$n alpine (yes)"
done
