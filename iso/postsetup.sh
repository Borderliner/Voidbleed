#!/bin/sh
# mklive -x hook: runs on the build host as root with the live rootfs as $1,
# after packages are installed and before the initramfs is generated.
set -eu
ROOTFS="$1"
LIVE_USER=anon

# Log the live user straight into niri once; logging out shows the greeter.
cat >"$ROOTFS/etc/greetd/config.toml" <<CONFIG
# Voidbleed live ISO
[terminal]
vt = 7

[initial_session]
command = "/usr/bin/voidbleed-session"
user = "$LIVE_USER"

[default_session]
command = "/usr/bin/voidbleed-greeter-session"
user = "_greeter"
CONFIG

# voidbleed-config's /etc/sudoers.d/wheel asks for a password and sorts after
# mklive's 99-void-live rule, and sudo applies the last match. Re-grant
# passwordless sudo to the live user from a file that sorts last.
cat >"$ROOTFS/etc/sudoers.d/zz-voidbleed-live" <<SUDOERS
# live ISO only
$LIVE_USER ALL=(ALL:ALL) NOPASSWD: ALL
SUDOERS
chmod 0440 "$ROOTFS/etc/sudoers.d/zz-voidbleed-live"

# The live user is created from /etc/skel at boot, always as /home/anon.
sed -i "s|@HOME@|/home/$LIVE_USER|g" "$ROOTFS/etc/skel/.config/qt6ct/qt6ct.conf"

# Record what the greeter's own setup produced, to check the palette seed.
echo "== voidbleed postsetup: /var/lib/noctalia-greeter"
ls -la "$ROOTFS/var/lib/noctalia-greeter" || true
grep -A3 '^\s*\[appearance.palette\]' "$ROOTFS/var/lib/noctalia-greeter/sync.toml" || true
