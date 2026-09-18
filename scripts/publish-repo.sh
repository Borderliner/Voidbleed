#!/usr/bin/env bash
# Publish packages to the public Voidbleed repository.
#
#   scripts/publish-repo.sh [pkg...]
#
# Takes packages out of build/repo, signs an index for them in build/void-repo,
# and copies the result to the server behind https://void.7mm.ir. With no
# names, it publishes whatever is listed in PUBLIC_PACKAGES below: the things
# that make sense on a machine that is not running Voidbleed.
#
#   DRY_RUN=1   build the repository locally and stop before uploading

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../packages/config.env
source "$root/packages/config.env"

# What a stock Void machine can use. Everything else in build/repo describes a
# Voidbleed system and would only confuse someone who installed it by hand.
PUBLIC_PACKAGES=(voidbleed-control voidbleed-plymouth-theme)

host="${VOIDBLEED_REPO_HOST:-7mm}"
remote="${VOIDBLEED_REPO_PATH:-/srv/void-repo}"
staging="$root/build/void-repo"
repo="$root/build/repo"

log() { printf '\033[1;31m::\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

pkgs=("$@")
[ ${#pkgs[@]} -gt 0 ] || pkgs=("${PUBLIC_PACKAGES[@]}")

[ -r "$VOIDBLEED_SIGNING_KEY" ] || die "signing key not found: $VOIDBLEED_SIGNING_KEY"

log "collecting ${pkgs[*]}"
rm -rf "$staging/current"
mkdir -p "$staging/current"
for name in "${pkgs[@]}"; do
    version="$(sed -n 's/^version=//p' "$root/packages/srcpkgs/$name/template" | head -1)"
    revision="$(sed -n 's/^revision=//p' "$root/packages/srcpkgs/$name/template" | head -1)"
    file="$repo/$name-${version}_${revision}.x86_64.xbps"
    [ -e "$file" ] || die "not built: $(basename "$file") — run scripts/build-packages.sh $name"
    cp -f "$file" "$staging/current/"
done

log "signing the index"
xbps-rindex -a "$staging"/current/*.xbps >/dev/null
xbps-rindex --privkey "$VOIDBLEED_SIGNING_KEY" --sign \
    --signedby "$VOIDBLEED_MAINTAINER" "$staging/current" >/dev/null
xbps-rindex --privkey "$VOIDBLEED_SIGNING_KEY" --sign-pkg "$staging"/current/*.xbps >/dev/null
cp -f "$root"/packages/keys/3e:24:*.plist "$staging/voidbleed-key.plist"

for name in "${pkgs[@]}"; do
    printf '   %s\n' "$(xbps-query --repository="$staging/current" -R -p pkgver "$name")"
done

if [ -n "${DRY_RUN:-}" ]; then
    log "built $staging (DRY_RUN, not uploaded)"
    exit 0
fi

# Sent as one archive: the repository is small, and a half-copied index is a
# repository nobody can install from.
log "uploading to $host:$remote"
tarball="$(mktemp -u /tmp/void-repo-XXXXXX.tar.gz)"
tar czf - -C "$staging" . | ssh "$host" "cat >$tarball"
ssh "$host" "set -e
    rm -rf $remote/current
    tar xzf $tarball -C $remote
    rm -f $tarball
    find $remote -type f | sed 's|$remote/||'"

log "done: https://void.7mm.ir/current"
