#!/usr/bin/env bash
# Build the Voidbleed live ISO with void-mklive. Must run as root:
#
#   sudo scripts/build-iso.sh [--keep]
#
# Prerequisite (as your normal user): scripts/build-packages.sh, so build/repo
# holds the voidbleed-* packages.
#
# A fresh copy of void-mklive (build/void-mklive, pinned in iso/mklive.rev) is
# patched with Voidbleed branding in build/iso-work, then run with the package
# set from the catalog. Output: build/voidbleed-live-x86_64-<date>.iso
#
#   --fast          test build: lz4 squashfs + gzip initramfs instead of xz.
#                   Much faster to compress, noticeably larger ISO (-fast.iso)
#   --keep          keep mklive's build directory (rootfs) for inspection
#   --prepare-only  patch void-mklive and print the package set, no root needed

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mklive_src="$root/build/void-mklive"
work="$root/build/iso-work"
repo="$root/build/repo"
cache="$root/build/xbps-cache-iso"
host_cache="$root/build/xbps-cache-host"
date="$(date -u +%Y%m%d)"

log() { printf '\033[1;31m::\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

keep=()
compression=(-s xz -i xz)
suffix=""
prepare_only=0
for arg in "$@"; do
    case "$arg" in
        --keep) keep=(-K) ;;
        --fast) compression=(-s lz4 -i gzip); suffix="-fast" ;;
        --prepare-only) prepare_only=1; work="$root/build/iso-prepare" ;;
        *) die "unknown argument $arg" ;;
    esac
done

out="$root/build/voidbleed-live-x86_64-$date$suffix.iso"

[ "$(id -u)" = 0 ] || [ "$prepare_only" = 1 ] || die "run as root: sudo $0"
[ -e "$repo/x86_64-repodata" ] || die "no package repo; run scripts/build-packages.sh as your user first"
owner="${SUDO_USER:-$(id -un)}"
# Run git as the invoking user so build/void-mklive stays user-owned.
as_owner() { if [ "$(id -u)" = 0 ] && [ "$owner" != root ]; then sudo -u "$owner" "$@"; else "$@"; fi; }

# ── void-mklive checkout, pinned ────────────────────────────────────────────
rev="$(sed -n 's/^\([0-9a-f]\{7,\}\).*/\1/p' "$root/iso/mklive.rev")"
if [ ! -d "$mklive_src/.git" ]; then
    log "cloning void-mklive"
    as_owner git clone -q https://github.com/void-linux/void-mklive.git "$mklive_src"
fi
if ! as_owner git -C "$mklive_src" cat-file -e "$rev^{commit}" 2>/dev/null; then
    as_owner git -C "$mklive_src" fetch -q --unshallow 2>/dev/null \
        || as_owner git -C "$mklive_src" fetch -q origin
fi

log "preparing void-mklive $rev in $work"
rm -rf "$work"
mkdir -p "$work" "$cache" "$host_cache"
git -C "$mklive_src" archive "$rev" | tar -x -C "$work"
cd "$work"

# ── Voidbleed patches ───────────────────────────────────────────────────────
python3 - "$root" <<'PATCH'
import pathlib, re, sys
root = pathlib.Path(sys.argv[1])

def patch(path, old, new, count=1, regex=False):
    p = pathlib.Path(path)
    s = p.read_text()
    n = len(re.findall(old, s, flags=re.M)) if regex else s.count(old)
    if n != count:
        sys.exit(f"patch failed: {path}: expected {count} match(es) of {old!r}, found {n}")
    p.write_text(re.sub(old, new, s, flags=re.M) if regex else s.replace(old, new))

# Live identity: hostname and default passwords.
patch("dracut/vmklive/adduser.sh", "echo void-live >", "echo voidbleed-live >")
patch("dracut/vmklive/adduser.sh", "voidlinux", "voidbleed", count=3)

# No speech-synthesis packages ship on Voidbleed, so drop those boot entries.
patch("mklive.sh", r'^\s*write_entry "\$\{ENTRY_TITLE\} with speech.*\n.*\n', "", count=3, regex=True)
patch("isolinux/isolinux.cfg.in", r"^LABEL linuxa11y\w*\n(?:(?!LABEL ).*\n)*", "", count=3, regex=True)

# BIOS menu colours (#AARRGGBB) and position below the splash's drip band.
patch("isolinux/isolinux.cfg.in", "#FF5255FF", "#FFE8313F", count=2)
patch("isolinux/isolinux.cfg.in", "MENU VSHIFT 2", "MENU VSHIFT 8")

# Console greeting.
for name in ("issue", "motd"):
    (pathlib.Path("data") / name).write_text((root / "iso" / name).read_text())
PATCH

# Boot splash: the desktop wallpaper, darkened so menu text stays readable.
python3 "$root/scripts/pngtool.py" fit \
    "$root/overlays/desktop/usr/share/backgrounds/voidbleed/voidbleed1.png" \
    data/splash.png 640 480 --darken 0.72
# Trust the Voiders and Voidbleed repo keys inside the image.
cp "$root"/packages/keys/*.plist keys/

# ── package and service sets from the catalog ──────────────────────────────
catalog="$root/scripts/catalog.py"
mapfile -t groups < <(grep -vE '^[[:space:]]*(#|$)' "$root/iso/live-groups.txt")
packages=(voidbleed-desktop)
mapfile -t -O ${#packages[@]} packages < <("$catalog" list defaults.txt)
mapfile -t -O ${#packages[@]} packages < <("$catalog" group packages "${groups[@]}")
services=()
mapfile -t services < <("$catalog" list services.txt)
mapfile -t -O ${#services[@]} services < <("$catalog" group services "${groups[@]}")

kernel="$("$catalog" kernel)"

log "kernel: $kernel"
log "packages: ${packages[*]}"
log "services: ${services[*]}"

[ "$prepare_only" = 1 ] && { log "prepared $work (--prepare-only)"; exit 0; }

# ── build ───────────────────────────────────────────────────────────────────
./mklive.sh "${keep[@]}" \
    -T "Voidbleed" \
    -v "$kernel" \
    -o "$out" \
    -c "$cache" \
    -H "$host_cache" \
    "${compression[@]}" \
    -r "$repo" \
    -r https://repo.voiders.dev \
    -p "${packages[*]}" \
    -S "${services[*]}" \
    -I "$root/iso/include" \
    -x "$root/iso/postsetup.sh" \
    -C "live.shell=/usr/bin/fish"

chown "$owner:" "$out"
chown -R "$owner:" "$work"
(cd "$(dirname "$out")" && sha256sum "$(basename "$out")") >"$out.sha256"
chown "$owner:" "$out.sha256"
log "ISO: $out ($(du -h "$out" | cut -f1))"
