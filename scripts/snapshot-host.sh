#!/usr/bin/env bash
# Capture the state of a reference Void machine so Voidbleed's defaults can be
# compared against it (see: scripts/catalog.py drift <snapshot-dir>).
#
# Only allowlisted files are copied. Secrets, histories, browser profiles and
# fish_variables are never collected. The output goes to snapshot/, which is
# gitignored; review it before sharing.
#
# Usage: scripts/snapshot-host.sh [output-dir]

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="${1:-$root/snapshot/$(hostname)-$(date +%Y%m%d-%H%M%S)}"
cfg="${XDG_CONFIG_HOME:-$HOME/.config}"
state="${XDG_STATE_HOME:-$HOME/.local/state}"

mkdir -p "$out"/{system,user,meta}

log() { printf '\033[1;31m::\033[0m %s\n' "$*"; }

# Copy a path into dest, keeping its absolute path. Unreadable or missing
# paths are recorded instead of failing the whole run.
grab() {
    local dest="$1"; shift
    local p
    for p in "$@"; do
        if [ -e "$p" ] || [ -L "$p" ]; then
            cp -a --parents "$p" "$dest" 2>>"$out/meta/skipped.txt" \
                || echo "unreadable: $p" >>"$out/meta/skipped.txt"
        else
            echo "missing: $p" >>"$out/meta/skipped.txt"
        fi
    done
}

log "machine"
{
    echo "date: $(date -Iseconds)"
    echo "kernel: $(uname -r)"
    echo "arch: $(uname -m)"
    grep -m1 'model name' /proc/cpuinfo | sed 's/^model name\s*:/cpu:/'
    if ls /sys/class/power_supply/BAT* >/dev/null 2>&1; then echo "chassis: laptop"; else echo "chassis: desktop"; fi
    [ -d /sys/firmware/efi ] && echo "firmware: uefi" || echo "firmware: bios"
    [ -d /sys/class/bluetooth ] && [ -n "$(ls -A /sys/class/bluetooth)" ] && echo "bluetooth: yes" || echo "bluetooth: no"
    echo "shell: $(getent passwd "$USER" | cut -d: -f7)"
} >"$out/meta/machine.txt"
lspci -nn >"$out/meta/lspci.txt" 2>/dev/null || true
lsblk -f -o NAME,FSTYPE,SIZE,MOUNTPOINTS >"$out/meta/lsblk.txt" 2>/dev/null || true

log "packages"
xbps-query -m | sort >"$out/meta/packages-manual-versions.txt"
sed -E 's/-[^-]+_[0-9]+$//' "$out/meta/packages-manual-versions.txt" >"$out/meta/packages-manual.txt"
xbps-query -l | awk '{print $2}' | sort >"$out/meta/packages-all.txt"
xbps-query -L >"$out/meta/repositories.txt" 2>/dev/null || true

log "services"
for s in /var/service/*; do
    [ -e "$s" ] || continue
    printf '%s\n' "$(basename "$s")"
done | sort >"$out/meta/services.txt"

log "flatpak"
if command -v flatpak >/dev/null; then
    flatpak list --app --columns=application,name,origin >"$out/meta/flatpak-apps.txt" 2>/dev/null || true
    flatpak remotes --columns=name,url >"$out/meta/flatpak-remotes.txt" 2>/dev/null || true
fi

log "system config"
grab "$out/system" \
    /etc/xbps.d \
    /etc/greetd \
    /etc/pipewire \
    /etc/alsa/conf.d \
    /etc/modprobe.d \
    /etc/dracut.conf.d \
    /etc/sysctl.d \
    /etc/rc.conf \
    /etc/locale.conf \
    /etc/default/grub \
    /etc/default/libc-locales \
    /etc/doas.conf \
    /etc/sudoers.d \
    /etc/polkit-1/rules.d \
    /etc/tlp.conf \
    /etc/tlp.d \
    /var/lib/noctalia-greeter/greeter.toml \
    /var/lib/noctalia-greeter/sync.toml

log "user config"
grab "$out/user" \
    "$cfg/niri" \
    "$cfg/noctalia" \
    "$state/noctalia/settings.toml" \
    "$cfg/ghostty" \
    "$cfg/foot" \
    "$cfg/fish/config.fish" \
    "$cfg/fish/conf.d" \
    "$cfg/fish/functions" \
    "$cfg/starship.toml" \
    "$cfg/gtk-3.0/settings.ini" \
    "$cfg/gtk-4.0/settings.ini" \
    "$cfg/qt6ct/qt6ct.conf" \
    "$cfg/mimeapps.list" \
    "$cfg/btop/btop.conf" \
    "$cfg/Thunar/uca.xml" \
    "$cfg/Thunar/accels.scm" \
    "$cfg/mpv" \
    "$cfg/autostart"

touch "$out/meta/skipped.txt"
log "snapshot written to $out"
log "compare with: scripts/catalog.py drift $out"
