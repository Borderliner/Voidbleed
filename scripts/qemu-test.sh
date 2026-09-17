#!/usr/bin/env bash
# Boot the Voidbleed live ISO in QEMU.
#
#   scripts/qemu-test.sh [options] [iso]
#
#   (no options)        UEFI, KVM, virtio GPU with OpenGL in a GTK window
#   --no-gl             virtio GPU without host OpenGL (software rendering in the guest)
#   --bios              legacy BIOS boot instead of UEFI
#   --disk              attach build/test-disk.qcow2 (created, 32G) for installer tests
#   --shots N[,N...]    headless: take screenshots N seconds after start into
#                       build/screenshots/, then power off
#   --rendernode PATH   host render node for virgl (default: first non-NVIDIA one)
#
# niri refuses software EGL, so the guest needs virgl (the default GL path).
# With --no-gl the compositor starts but cannot render; that mode is only
# useful for console tests.
#
# Needs: qemu-system-amd64 edk2-ovmf (sudo xbps-install -S qemu-system-amd64 edk2-ovmf)
# and membership in the kvm group.

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ovmf_code=/usr/share/edk2/x64/OVMF_CODE.4m.fd
ovmf_vars=/usr/share/edk2/x64/OVMF_VARS.4m.fd

gl=1 bios=0 disk=0 shots="" iso="" rendernode=""
while [ $# -gt 0 ]; do
    case "$1" in
        --no-gl) gl=0 ;;
        --bios) bios=1 ;;
        --disk) disk=1 ;;
        --shots) shots="$2"; shift ;;
        --rendernode) rendernode="$2"; shift ;;
        -*) echo "unknown option $1" >&2; exit 2 ;;
        *) iso="$1" ;;
    esac
    shift
done
[ -n "$iso" ] || iso="$(ls -t "$root"/build/voidbleed-live-*.iso 2>/dev/null | head -1)"
[ -f "$iso" ] || { echo "error: no ISO found; build one with: sudo scripts/build-iso.sh" >&2; exit 1; }
command -v qemu-system-x86_64 >/dev/null || { echo "error: install qemu-system-amd64" >&2; exit 1; }

# QEMU's virgl crashes on this host if it picks the NVIDIA node, so prefer
# an Intel/AMD render node unless one was given.
if [ -z "$rendernode" ]; then
    for n in /sys/class/drm/renderD*; do
        [ -e "$n/device/vendor" ] || continue
        case "$(cat "$n/device/vendor")" in
            0x10de) continue ;;
            *) rendernode="/dev/dri/$(basename "$n")"; break ;;
        esac
    done
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

args=(
    -name Voidbleed
    -machine q35,accel=kvm -cpu host -smp 4 -m 4G
    -cdrom "$iso" -boot order=d
    -nic user,model=virtio-net-pci
    -device qemu-xhci -device usb-tablet
    -audiodev none,id=noaudio -device intel-hda -device hda-output,audiodev=noaudio
)

if [ "$bios" = 0 ]; then
    [ -f "$ovmf_code" ] || { echo "error: install edk2-ovmf" >&2; exit 1; }
    cp "$ovmf_vars" "$tmp/vars.fd"
    args+=(-drive "if=pflash,format=raw,readonly=on,file=$ovmf_code"
           -drive "if=pflash,format=raw,file=$tmp/vars.fd")
fi

if [ "$disk" = 1 ]; then
    img="$root/build/test-disk.qcow2"
    [ -f "$img" ] || qemu-img create -f qcow2 "$img" 32G >/dev/null
    args+=(-drive "file=$img,if=virtio,format=qcow2")
fi

if [ -z "$shots" ]; then
    if [ "$gl" = 1 ]; then
        args+=(-device virtio-vga-gl -display gtk,gl=on)
    else
        args+=(-device virtio-vga -display gtk)
    fi
    exec qemu-system-x86_64 "${args[@]}"
fi

# ── headless screenshots ────────────────────────────────────────────────────
# Frames are grabbed over VNC: QEMU's screendump cannot capture a virgl scanout.
out="$root/build/screenshots"
mkdir -p "$out"
if [ "$gl" = 1 ]; then
    args+=(-device virtio-vga-gl -display "egl-headless${rendernode:+,rendernode=$rendernode}")
else
    args+=(-device virtio-vga -display none)
fi
vnc_display="${VNC_DISPLAY:-9}"
args+=(-vnc "127.0.0.1:$vnc_display" -monitor "unix:$tmp/monitor,server,nowait")

log="$root/build/qemu.log"
: >"$log"
qemu-system-x86_64 "${args[@]}" >"$log" 2>&1 &
qemu=$!

start=$(date +%s)
rc=0
for t in ${shots//,/ }; do
    while [ $(( $(date +%s) - start )) -lt "$t" ]; do
        kill -0 "$qemu" 2>/dev/null || { echo "qemu exited early; see build/qemu.log"; exit 1; }
        sleep 1
    done
    if ! python3 "$root/scripts/vnc-shot.py" "127.0.0.1:$((5900 + vnc_display))" "$out/t$(printf %04d "$t").png"; then
        echo "screenshot at ${t}s failed"
        rc=1
    fi
done

printf 'quit\n' | timeout 5 socat - "unix-connect:$tmp/monitor" >/dev/null 2>&1 || kill "$qemu" 2>/dev/null
wait "$qemu" 2>/dev/null || true
exit "$rc"
