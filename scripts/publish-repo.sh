#!/usr/bin/env bash
# Publish the Voidbleed packages to the server behind https://void.7mm.ir.
#
#   scripts/publish-repo.sh
#
# Two signed repositories, from the same key and the same build/repo:
#
#   current/    what a stock Void machine can use -- the control centre and
#               the boot splash. This is the one other people add.
#   voidbleed/  every package the ISO is built from, the config packages
#               included. Installed machines point here (10-voidbleed.conf),
#               so a fix to voidbleed-desktop-config reaches them instead of
#               waiting for the next ISO.
#
# The split is deliberate: voidbleed-config rebrands a machine and pins its
# kernel, which is not something a passing Void user should be able to install
# by name from a general-purpose repository.
#
#   DRY_RUN=1   build both repositories locally and stop before uploading

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../packages/config.env
source "$root/packages/config.env"

PUBLIC_PACKAGES=(voidbleed-control voidbleed-plymouth-theme)

host="${VOIDBLEED_REPO_HOST:-7mm}"
remote="${VOIDBLEED_REPO_PATH:-/srv/void-repo}"
staging="$root/build/void-repo"
repo="$root/build/repo"

log() { printf '\033[1;31m::\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

[ -r "$VOIDBLEED_SIGNING_KEY" ] || die "signing key not found: $VOIDBLEED_SIGNING_KEY"

# The package built for a template's current version and revision, so a
# half-finished rebuild publishes nothing rather than something stale.
pkgfile() {
    local name="$1" version revision file
    version="$(sed -n 's/^version=//p' "$root/packages/srcpkgs/$name/template" | head -1)"
    revision="$(sed -n 's/^revision=//p' "$root/packages/srcpkgs/$name/template" | head -1)"
    file="$repo/$name-${version}_${revision}.x86_64.xbps"
    [ -e "$file" ] || die "not built: $(basename "$file") -- run scripts/build-packages.sh $name"
    printf '%s\n' "$file"
}

# Copy packages into a repository of their own and sign it. The index is built
# from the files present, so a revision that is no longer shipped disappears.
publish_dir() {
    local dir="$1"; shift
    rm -rf "$dir"
    mkdir -p "$dir"
    cp -f "$@" "$dir/"
    xbps-rindex -a "$dir"/*.xbps >/dev/null
    xbps-rindex --privkey "$VOIDBLEED_SIGNING_KEY" --sign \
        --signedby "$VOIDBLEED_MAINTAINER" "$dir" >/dev/null
    xbps-rindex --privkey "$VOIDBLEED_SIGNING_KEY" --sign-pkg "$dir"/*.xbps >/dev/null
    for pkg in "$dir"/*.xbps; do
        printf '   %s\n' "$(basename "$pkg" .x86_64.xbps)"
    done
}

public=()
for name in "${PUBLIC_PACKAGES[@]}"; do
    public+=("$(pkgfile "$name")")
done

all=("$repo"/*.xbps)
[ -e "${all[0]}" ] || die "nothing built -- run scripts/build-packages.sh"

log "current/ -- ${#public[@]} packages for any Void machine"
publish_dir "$staging/current" "${public[@]}"

log "voidbleed/ -- ${#all[@]} packages for Voidbleed machines"
publish_dir "$staging/voidbleed" "${all[@]}"

cp -f "$root"/packages/keys/3e:24:*.plist "$staging/voidbleed-key.plist"

if [ -n "${DRY_RUN:-}" ]; then
    log "built $staging (DRY_RUN, not uploaded)"
    exit 0
fi

# Sent as one archive: the repositories are small, and a half-copied index is
# a repository nobody can install from.
log "uploading to $host:$remote"
tarball="$(mktemp -u /tmp/void-repo-XXXXXX.tar.gz)"
tar czf - -C "$staging" . | ssh "$host" "cat >$tarball"
ssh "$host" "set -e
    rm -rf $remote/current $remote/voidbleed
    tar xzf $tarball -C $remote
    rm -f $tarball
    du -sh $remote/current $remote/voidbleed"

log "done: https://void.7mm.ir/current and https://void.7mm.ir/voidbleed"
