#!/usr/bin/env bash
# Build the Voidbleed package repository.
#
#   scripts/build-packages.sh [--update] [pkg...]
#
# Stages packages/srcpkgs into a void-packages checkout (build/void-packages),
# fills in the @REPO_URL@/@HOMEPAGE@/@MAINTAINER@ placeholders from
# packages/config.env, builds with xbps-src, and publishes a signed repository
# in build/repo. With no package names, everything in packages/srcpkgs is built.
#
#   --update   refresh build/void-packages to the latest upstream master first

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../packages/config.env
source "$root/packages/config.env"

vp="$root/build/void-packages"
repo="$root/build/repo"
keys="$root/packages/keys"

log() { printf '\033[1;31m::\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

update=0
pkgs=()
for arg in "$@"; do
    case "$arg" in
        --update) update=1 ;;
        -*) die "unknown option $arg" ;;
        *) pkgs+=("$arg") ;;
    esac
done
if [ ${#pkgs[@]} -eq 0 ]; then
    for d in "$root"/packages/srcpkgs/*/; do pkgs+=("$(basename "$d")"); done
fi

[ -r "$VOIDBLEED_SIGNING_KEY" ] || die "signing key not found: $VOIDBLEED_SIGNING_KEY"
"$root/scripts/catalog.py" sync-templates --check >/dev/null \
    || die "metapackage templates are stale; run scripts/catalog.py sync-templates"

# ── void-packages checkout ──────────────────────────────────────────────────
if [ ! -d "$vp/.git" ]; then
    log "cloning void-packages"
    git clone --depth 1 https://github.com/void-linux/void-packages.git "$vp"
elif [ "$update" = 1 ]; then
    log "updating void-packages"
    git -C "$vp" fetch --depth 1 origin master
    git -C "$vp" checkout -q --detach FETCH_HEAD
fi
cat >"$vp/etc/conf" <<EOF
XBPS_MAKEJOBS=$(nproc)
XBPS_MIRROR=https://repo-de.voidlinux.org/current
XBPS_CCACHE=yes
EOF
[ -d "$vp/masterdir-x86_64" ] || { log "bootstrapping build chroot"; "$vp/xbps-src" binary-bootstrap; }

# ── stage templates ─────────────────────────────────────────────────────────
stage() {
    local name="$1" dest="$vp/srcpkgs/$1"
    rm -rf "$dest"
    cp -a "$root/packages/srcpkgs/$name" "$dest"
    mkdir -p "$dest/files"
    # Voidbleed's own packages are MIT; ship the text with them.
    case "$name" in
        voidbleed-*) cp -a "$root/LICENSE" "$dest/files/LICENSE" ;;
    esac
    case "$name" in
        voidbleed-config)
            mkdir -p "$dest/files/keys"
            cp -a "$root/overlays/system" "$dest/files/overlay"
            # No hosted repository yet: don't point installed systems at one.
            [ -n "$VOIDBLEED_REPO_URL" ] || rm -f "$dest/files/overlay/usr/share/xbps.d/10-voidbleed.conf"
            cp -a "$keys"/*.plist "$dest/files/keys/" 2>/dev/null || true
            ;;
        voidbleed-desktop-config) cp -a "$root/overlays/desktop" "$dest/files/overlay" ;;
        voidbleed-nvidia-config)  cp -a "$root/overlays/nvidia" "$dest/files/overlay" ;;
        reversal-red-icon-theme)
            cp -a "$root/packages/assets/Reversal-red.tar.xz" "$dest/files/"
            ;;
        voidbleed-installer)
            log "building the installer binary"
            (cd "$root/installer" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" \
                -o "$dest/files/voidbleed-installer" ./cmd/voidbleed-installer)
            cp -a "$root/catalog" "$dest/files/catalog"
            ;;
    esac
    grep -rlZ -e '@REPO_URL@' -e '@HOMEPAGE@' -e '@MAINTAINER@' "$dest" 2>/dev/null \
        | xargs -0 -r sed -i \
            -e "s|@REPO_URL@|$VOIDBLEED_REPO_URL|g" \
            -e "s|@HOMEPAGE@|$VOIDBLEED_HOMEPAGE|g" \
            -e "s|@MAINTAINER@|$VOIDBLEED_MAINTAINER|g"
}

# ── build ───────────────────────────────────────────────────────────────────
build() {
    local name="$1" force=()
    log "building $name"
    # Voidbleed's own packages are cheap and change without version bumps
    # during development, so always rebuild them from a clean destdir.
    case "$name" in
        voidbleed-*) force=(-f); "$vp/xbps-src" clean "$name" >/dev/null ;;
    esac
    "$vp/xbps-src" "${force[@]}" pkg "$name"
}

publish() {
    log "publishing to $repo"
    mkdir -p "$repo"
    local name ver rev f
    for name in "${pkgs[@]}"; do
        # Publish exactly the revision the template names. Globbing every
        # build in hostdir instead would leave xbps-rindex to order them, and
        # it reads _10 as older than _9.
        ver="$(sed -n 's/^version=//p' "$root/packages/srcpkgs/$name/template" | head -1)"
        rev="$(sed -n 's/^revision=//p' "$root/packages/srcpkgs/$name/template" | head -1)"
        f="$vp/hostdir/binpkgs/$name-${ver}_${rev}.x86_64.xbps"
        [ -e "$f" ] || die "not built: $(basename "$f")"
        rm -f "$repo/$name-"[0-9]*.xbps "$repo/$name-"[0-9]*.xbps.sig2
        cp -f "$f" "$repo/"
        # Voidbleed's packages are rebuilt without a revision bump, so a copy
        # left in the download cache no longer matches what is published.
        rm -f "$root/build/xbps-cache/$name-"[0-9]*.xbps*
    done
    # Rebuild the index from the files that are actually here: it then always
    # carries their real hashes, rebuild or not.
    rm -f "$repo"/*-repodata
    xbps-rindex -a "$repo"/*.xbps
    xbps-rindex --privkey "$VOIDBLEED_SIGNING_KEY" --sign --signedby "$VOIDBLEED_MAINTAINER" "$repo"
    xbps-rindex --privkey "$VOIDBLEED_SIGNING_KEY" --sign-pkg "$repo"/*.xbps
}

# Stage everything up front: xbps-src requires dependencies such as
# voidbleed-config to exist in srcpkgs even when they are built later.
for name in "${pkgs[@]}"; do stage "$name"; done

# voidbleed-config ships the repository public key, so it is built last: the
# first run exports the key from the freshly signed repo before building it.
config_requested=0
for name in "${pkgs[@]}"; do
    if [ "$name" = voidbleed-config ]; then config_requested=1; continue; fi
    build "$name"
done
[ ${#pkgs[@]} -gt "$config_requested" ] && publish

if [ "$config_requested" = 1 ]; then
    signer="$(sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g' <<<"$VOIDBLEED_MAINTAINER")"
    if ! grep -qsF "<string>$signer</string>" "$keys"/*.plist; then
        [ -e "$repo"/x86_64-repodata ] || die "build another package first so the key can be exported"
        "$root/scripts/export-repo-key.sh"
        stage voidbleed-config
    fi
    build voidbleed-config
    publish
fi

log "done: $(ls "$repo"/*.xbps | wc -l) packages in $repo"
