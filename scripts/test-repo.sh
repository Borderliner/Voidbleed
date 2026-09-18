#!/usr/bin/env bash
# Smoke-test build/repo without root.
#
# 1. Serves the repo over localhost so XBPS verifies its signature.
# 2. Resolves a full install (voidbleed-desktop + defaults) as a dry run.
# 3. Really installs the Voidbleed config packages into a scratch root inside a
#    user namespace, then checks the files and INSTALL-script results.
# 4. Force-reinstalls base-files to prove the os-release branding survives.
#
# KEEP=1 keeps the scratch root for inspection.

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo="$root/build/repo"
cache="$root/build/xbps-cache"
port="${PORT:-8766}"
tmp="$(mktemp -d)"
target="$tmp/root"
official=(--repository=https://repo-de.voidlinux.org/current
          --repository=https://repo-de.voidlinux.org/current/nonfree
          --repository=https://repo.voiders.dev)

cleanup() {
    [ -n "${server:-}" ] && kill "$server" 2>/dev/null || true
    if [ -n "${KEEP:-}" ]; then echo "kept scratch root: $target"; else rm -rf "$tmp"; fi
}
trap cleanup EXIT

fails=0
pass() { printf '  \033[32m✓\033[0m %s\n' "$*"; }
fail() { printf '  \033[31m✗\033[0m %s\n' "$*"; fails=$((fails + 1)); }
check() { local desc="$1"; shift; if "$@" >/dev/null 2>&1; then pass "$desc"; else fail "$desc"; fi; }

python3 -m http.server --bind 127.0.0.1 --directory "$repo" "$port" >/dev/null 2>&1 &
server=$!
sleep 1

mkdir -p "$target/var/db/xbps/keys" "$cache"
cp /var/db/xbps/keys/60:ae:0c:d6:f0:95:17:80:bc:93:46:7a:89:af:a3:2d.plist "$target/var/db/xbps/keys/"
cp "$root"/packages/keys/*.plist "$target/var/db/xbps/keys/"

xi() {
    unshare -r xbps-install -r "$target" -c "$cache" \
        --repository="http://127.0.0.1:$port" "${official[@]}" "$@"
}

echo "signature and dependency resolution"
xi -S >/dev/null
check "repo signature verified with the shipped key" \
    xbps-query -r "$target" --repository="http://127.0.0.1:$port" -R -p pkgver voidbleed-desktop
mapfile -t defaults < <("$root/scripts/catalog.py" list defaults.txt)
if xi -n voidbleed-desktop "${defaults[@]}" >"$tmp/dry-run.txt" 2>&1; then
    pass "voidbleed-desktop + defaults resolve ($(grep -c ' install ' "$tmp/dry-run.txt") packages)"
else
    fail "voidbleed-desktop + defaults resolve"; tail -5 "$tmp/dry-run.txt"
fi

echo "install config packages into a scratch root"
# The icon theme is large; install it too so its files are checked.
if xi -y bash coreutils grep sed findutils shadow \
        voidbleed-config voidbleed-desktop-config reversal-red-icon-theme xcursor-vanilla-dmz \
        plymouth >"$tmp/install.txt" 2>&1; then
    pass "transaction completed"
else
    fail "transaction completed"; tail -20 "$tmp/install.txt"
fi

t="$target"
check "os-release is Voidbleed"                grep -q '^NAME="Voidbleed"' "$t/usr/lib/os-release"
check "/etc/os-release resolves to it"         grep -q '^ID="*voidbleed' "$t/etc/os-release"
check "kernel metapackage ignored"             grep -q '^ignorepkg=linux$' "$t/usr/share/xbps.d/05-voidbleed-kernel.conf"
check "noextract rule installed"               grep -q '^noextract=/usr/lib/os-release' "$t/usr/share/xbps.d/05-voidbleed-noextract.conf"
check "no placeholder repo URL shipped"        bash -c "! grep -rqs '@REPO_URL@' '$t/usr/share/xbps.d'"
for k in "$root"/packages/keys/*.plist; do
    check "key $(basename "$k" .plist) installed" test -f "$t/var/db/xbps/keys/$(basename "$k")"
done
check "voiders repo configured"                grep -q '^repository=https://repo.voiders.dev' "$t/usr/share/xbps.d/20-voiders-community.conf"
check "new users default to fish"              grep -q '^SHELL=/usr/bin/fish$' "$t/etc/default/useradd"
check "fish is a valid login shell"            grep -qx /usr/bin/fish "$t/etc/shells"
check "vpm fish completion installed"          test -s "$t/usr/share/fish/vendor_completions.d/vpm.fish"
check "sudoers drop-in is 0440"                test "$(stat -c %a "$t/etc/sudoers.d/wheel")" = 440
check "greetd uses the Voidbleed greeter"      grep -q voidbleed-greeter-session "$t/etc/greetd/config.toml"
check "greeter palette seeded"                 grep -q 'primary = "#E8313F"' "$t/var/lib/noctalia-greeter/sync.toml"
check "skel niri config"                       test -f "$t/etc/skel/.config/niri/config.kdl"
check "skel Voidbleed palette"                 test -f "$t/etc/skel/.config/noctalia/palettes/Voidbleed.json"
check "skel uses the Reversal icon theme"      grep -q '^gtk-icon-theme-name=Reversal-red-dark$' "$t/etc/skel/.config/gtk-3.0/settings.ini"
check "skel uses the white DMZ cursor"         grep -q '^gtk-cursor-theme-name=Vanilla-DMZ$' "$t/etc/skel/.config/gtk-4.0/settings.ini"
check "Qt uses the same icon theme"            grep -q '^icon_theme=Reversal-red-dark$' "$t/etc/skel/.config/qt6ct/qt6ct.conf"
check "niri sets the white cursor"             grep -q 'xcursor-theme "Vanilla-DMZ"' "$t/etc/skel/.config/niri/config.kdl"
check "skel ghostty config"                    test -f "$t/etc/skel/.config/ghostty/config"
check "wallpaper installed"                    test -s "$t/usr/share/backgrounds/voidbleed/voidbleed1.png"
check "logo installed"                         test -s "$t/usr/share/pixmaps/voidbleed-logo.png"
check "skel skips Noctalia setup wizard"       test -f "$t/etc/skel/.local/state/noctalia/.setup-complete"
check "theme uses vibrant wallpaper colors"    grep -q '^wallpaper_scheme = "vibrant"' "$t/etc/skel/.config/noctalia/config.toml"
check "greeter wallpaper seeded"               grep -q 'backgrounds/voidbleed' "$t/var/lib/noctalia-greeter/sync.toml"
check "skel ~/.local/bin exists"               test -d "$t/etc/skel/.local/bin"
check "fish uses gnome-keyring SSH agent"      test -s "$t/usr/share/fish/vendor_conf.d/voidbleed-ssh-agent.fish"
check "PDFs open in Papers"                    grep -q '^application/pdf=org.gnome.Papers.desktop$' "$t/etc/xdg/mimeapps.list"
check "images open in gThumb"                  grep -q '^image/png=org.gnome.gThumb.desktop$' "$t/etc/xdg/mimeapps.list"
check "videos open in mpv"                     grep -q '^video/mp4=mpv.desktop$' "$t/etc/xdg/mimeapps.list"
check "session wrapper starts a dbus session"  grep -q 'exec dbus-run-session niri --session' "$t/usr/bin/voidbleed-session"
check "session wrapper is executable"          test -x "$t/usr/bin/voidbleed-session"
check "Voidbleed session entry installed"      grep -q '^Exec=/usr/bin/voidbleed-session' "$t/usr/share/wayland-sessions/voidbleed.desktop"
check "greeter wrapper releases the splash"    grep -q 'plymouth quit --retain-splash' "$t/usr/bin/voidbleed-greeter-session"
check "greeter wrapper is executable"          test -x "$t/usr/bin/voidbleed-greeter-session"
check "splash theme installed"                 test -s "$t/usr/share/plymouth/themes/voidbleed/voidbleed.script"
check "splash theme uses the script plugin"    grep -q '^ModuleName=script$' "$t/usr/share/plymouth/themes/voidbleed/voidbleed.plymouth"
check "splash theme selected"                  grep -q '^Theme=voidbleed$' "$t/etc/plymouth/plymouthd.conf"
check "plymouth lands in the initramfs"        grep -q 'add_dracutmodules+=" plymouth "' "$t/usr/lib/dracut/dracut.conf.d/05-voidbleed-splash.conf"
# The initramfs carries no fonts, so the splash draws pictures only: a missing
# one is a blank spot on a screen nobody can debug from.
while read -r image; do
    check "splash image $image installed"      test -s "$t/usr/share/plymouth/themes/voidbleed/$image"
done < <(grep -o 'Image("[^"]*")' "$root/overlays/system/usr/share/plymouth/themes/voidbleed/voidbleed.script" |
    sed 's/Image("//; s/")//' | sort -u)
check "greeter sync polkit rule"               grep -q 'org.noctalia.greeter.apply-appearance' "$t/usr/share/polkit-1/rules.d/49-noctalia-greeter-sync.rules"
# GTK reads its appearance through the portal, which reads GSettings: without
# these defaults the skeleton's settings.ini is ignored and apps look stock.
check "GSettings theme defaults shipped"       grep -q "^icon-theme='Reversal-red-dark'$" "$t/usr/share/glib-2.0/schemas/99-voidbleed.gschema.override"
check "GSettings cursor default shipped"       grep -q "^cursor-theme='Vanilla-DMZ'$" "$t/usr/share/glib-2.0/schemas/99-voidbleed.gschema.override"
check "Thunar opens Ghostty as its terminal"   grep -q '^TerminalEmulator=ghostty$' "$t/etc/skel/.config/xfce4/helpers.rc"
check "Reversal icon theme installed"          test -f "$t/usr/share/icons/Reversal-red-dark/index.theme"
check "white DMZ cursor installed"             test -d "$t/usr/share/icons/Vanilla-DMZ/cursors"
check "PipeWire links present"                 test -L "$t/etc/pipewire/pipewire.conf.d/20-pipewire-pulse.conf"
check "_greeter account created by greetd"     grep -q '^_greeter:' "$t/etc/passwd"

echo "new user account"
unshare -r chroot "$t" /usr/bin/useradd -D >"$tmp/useradd-defaults.txt" 2>&1 || true
check "useradd -D reports fish"                grep -q '^SHELL=/usr/bin/fish$' "$tmp/useradd-defaults.txt"
# Only the account entry is checked: copying /etc/skel into the new home
# stops at the first chown, because the new UID isn't mapped inside the
# unprivileged user namespace (useradd doesn't report it). The skeleton
# itself is checked above.
unshare -r chroot "$t" /usr/bin/useradd -m vbprobe >/dev/null 2>&1 || true
check "useradd creates a fish account"         grep -q '^vbprobe:.*:/usr/bin/fish$' "$t/etc/passwd"

echo "no file conflicts with the NVIDIA drivers"
xbps-query -r "$target" --repository="http://127.0.0.1:$port" "${official[@]}" -R --files voidbleed-nvidia-config \
    >"$tmp/nvidia-config-files.txt" 2>/dev/null || true
check "voidbleed-nvidia-config ships files"    test -s "$tmp/nvidia-config-files.txt"
for driver in nvidia nvidia580 nvidia580-dkms; do
    xbps-query -r "$target" --repository="http://127.0.0.1:$port" "${official[@]}" -R --files "$driver" \
        >"$tmp/$driver-files.txt" 2>/dev/null || true
    [ -s "$tmp/$driver-files.txt" ] || continue
    check "no files shared with $driver" \
        bash -c "! comm -12 <(sort '$tmp/nvidia-config-files.txt') <(sort '$tmp/$driver-files.txt') | grep -q ."
done

echo "kernel pinning with voidbleed-config installed"
kernel="$("$root/scripts/catalog.py" kernel)"
xi -n voidbleed-base >"$tmp/kernel.txt" 2>&1 || true
check "voidbleed-base pulls $kernel"            grep -q "^$kernel-[0-9]" "$tmp/kernel.txt"
check "linux metapackage not installed"        bash -c "! grep -qE '^linux-[0-9]' '$tmp/kernel.txt'"

echo "base-files update keeps branding"
xi -fy base-files >/dev/null 2>&1 || true
check "os-release still Voidbleed after reinstall" grep -q '^NAME="Voidbleed"' "$t/usr/lib/os-release"

echo
if [ "$fails" = 0 ]; then echo "all checks passed"; else echo "$fails check(s) failed"; exit 1; fi
