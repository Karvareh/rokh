#!/bin/sh
# Build every target, with checksums, into a directory named for the release.
#
# Reproducible on purpose. -trimpath keeps the builder's home directory out of
# the binary, and -buildvcs=true puts the commit in — so two people building the
# same commit get the same bytes, and anyone holding a binary can say which
# source it came from. A release nobody can reproduce is a release nobody can
# check.
#
# Usage:  ./build.sh [release-name]
set -eu

cd "$(dirname "$0")"
release="${1:-}"
if [ -z "$release" ]; then
    release="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
fi
out="releases/$release"

# Checksums need a tool, and two systems spell it two ways. Decide here, before
# a single target is built, rather than after: the old form silenced the tool's
# own complaint and let sort's exit status stand in for it, so a builder with
# neither tool wrote an empty SHA256SUMS.txt and said nothing. That release is
# uninstallable, and it fails on someone else's machine, not on this one.
if command -v shasum >/dev/null 2>&1; then
    sums() { shasum -a 256 "$@"; }
elif command -v sha256sum >/dev/null 2>&1; then
    sums() { sha256sum "$@"; }
else
    printf 'neither shasum nor sha256sum; no checksums, so no release\n' >&2
    exit 1
fi

if ! git diff --quiet 2>/dev/null || ! git diff --cached --quiet 2>/dev/null; then
    printf 'the tree has uncommitted changes; the build will say so\n' >&2
fi

rm -rf "$out"
mkdir -p "$out"

# One line per target: GOOS GOARCH.
targets='darwin arm64
darwin amd64
linux amd64
linux arm64
linux arm
freebsd amd64'

# Every command a person might actually want, not only the main one.
commands='rokh rokh-shell rokh-courier rokh-forms'

# The chest is a LUKS2 header and a btrfs filesystem inside one file, and both
# belong to the Linux kernel. It is built for Linux and nowhere else, rather
# than shipping a binary elsewhere whose whole behaviour is to say no.
linux_only='rokh-chest'

printf 'building %s\n' "$release"
echo "$targets" | while read -r goos goarch; do
    [ -n "$goos" ] || continue
    for cmd in $commands $( [ "$goos" = linux ] && echo "$linux_only" ); do
        name="$cmd-$goos-$goarch"
        if ! CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
            go build -trimpath -buildvcs=true \
            -ldflags "-s -w -X main.Release=$release" \
            -o "$out/$name" "./cmd/$cmd" 2>/dev/null; then
            printf '  %-32s skipped\n' "$name"
            continue
        fi
        printf '  %-32s %s\n' "$name" "$(du -h "$out/$name" | cut -f1)"
    done
done

# Checksums, so a person who was handed a binary can tell whether it is the one
# that was built. The list is sorted, so the file itself is comparable between
# two builds of the same release.
# The list is taken while the file does not exist yet, so it can never hold a
# line for itself, and a failing tool stops the build instead of being sorted
# into an empty file.
list=$(cd "$out" && sums *)
printf '%s\n' "$list" | sort -k2 > "$out/SHA256SUMS.txt"

printf '\n%s\n' "$out"
cat "$out/SHA256SUMS.txt"
