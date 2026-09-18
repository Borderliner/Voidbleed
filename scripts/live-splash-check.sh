# Render the Voidbleed boot splash inside the live VM, so the theme can be
# checked without building an ISO first.
#
#   scripts/vm-install-test.py --live-script scripts/live-splash-check.sh \
#       --shot-every 6 --shot-key a --out build/vm-splash
#
# $BASE is the host's HTTP server, set by the harness.

mkdir -p /usr/share/voidbleed /run/plymouth
curl -fsS "$BASE/repo.tar" | tar -x -C /usr/share/voidbleed

# plymouth comes from the mirror; the theme and plymouthd.conf come from the
# freshly built voidbleed-config.
xbps-install -Sy plymouth
xbps-install -y --repository=/usr/share/voidbleed/repo -u voidbleed-config
ls -l /usr/share/plymouth/themes/voidbleed/
cat /etc/plymouth/plymouthd.conf

# The theme is only useful if dracut puts it in the initramfs: that is where
# it draws, and where it asks for the passphrase of an encrypted root.
dracut -N --force /tmp/test-initrd "$(uname -r)" 2>&1 | tail -3
lsinitrd /tmp/test-initrd 2>/dev/null | grep -E 'plymouth(d|/)' | head -20
lsinitrd /tmp/test-initrd 2>/dev/null | grep -c 'themes/voidbleed'

# greetd holds the display; plymouth needs it to draw.
sv down greetd
sleep 5

# plymouth only draws a themed splash when the kernel command line says
# "splash" (installed systems get it from the installer, the ISO from
# build-iso.sh); this ISO predates that, so tell plymouthd directly.
plymouthd --mode=boot --debug --debug-file=/tmp/ply.log --kernel-command-line="splash"
# plymouth listens for keystrokes on tty1; the harness types on whatever vt is
# active, and this script runs on tty3. At boot plymouth owns the active vt and
# no getty is running yet, so both are arranged here.
sv down agetty-tty1 || true
chvt 1 || true
plymouth show-splash
sleep 24

# The passphrase screen: the harness types into it, so bullets appear.
plymouth ask-for-password --prompt "check" --number-of-tries=1 >/dev/null 2>&1 &
sleep 24

plymouth quit --retain-splash
sleep 2
grep -iE 'error|invalid|not found|parse' /tmp/ply.log | head -20
curl -fsS --data-binary @/tmp/ply.log "$BASE/upload/ply.log"
