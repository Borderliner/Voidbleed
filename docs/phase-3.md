# Phase 3 — live ISO

## Build and test

```sh
scripts/build-packages.sh                 # as your user: voidbleed-* into build/repo
sudo scripts/build-iso.sh                 # release build (xz), mklive needs root
sudo scripts/build-iso.sh --fast          # test build: lz4 squashfs + gzip initramfs
scripts/qemu-test.sh                      # UEFI + virtio GPU with OpenGL, GTK window
scripts/qemu-test.sh --shots 40,90,150    # headless screenshots into build/screenshots
```

Host packages for testing: `qemu-system-amd64 edk2-ovmf`.

Build time: the target and host package caches live in `build/xbps-cache-iso`
and `build/xbps-cache-host`, so only changed packages are downloaded. `--fast`
skips the slow xz compression of the ~5 GB rootfs at the cost of a larger ISO;
use it for VM testing and xz for anything you hand out. ISO rebuilds are
batched: package-level changes are verified with `scripts/test-repo.sh` and
only land in an ISO when there is something boot-level to check.

## What the ISO contains

- kernel: the series pinned in `catalog/base.txt` (`mklive.sh -v linux6.18`,
  which ignores the `linux` metapackage in the image)
- `voidbleed-desktop` + `catalog/defaults.txt`
- optional groups from `iso/live-groups.txt` (Intel/AMD video acceleration,
  Bluetooth, power profiles, Firefox). No proprietary NVIDIA: nouveau keeps the
  live session bootable on any GPU.
- services from `catalog/services.txt` plus those groups' services

## How it is built

`scripts/build-iso.sh` copies void-mklive at the commit in `iso/mklive.rev`
into `build/iso-work` and patches it:

| Patch | Why |
|---|---|
| hostname `voidbleed-live`, passwords `voidbleed` | live identity (`dracut/vmklive/adduser.sh`) |
| speech boot entries removed (GRUB + isolinux) | no speech packages ship |
| isolinux colours `#E8313F`, menu moved down | brand; clears the splash drip band |
| `data/splash.png` from the wallpaper via `scripts/pngtool.py` | boot splash matches the desktop |
| `data/issue`, `data/motd` from `iso/` | console greeting |
| Voiders + Voidbleed keys added to `keys/` | trusted inside the image |

Each patch asserts its match count, so an upstream change fails the build
instead of silently producing an unbranded ISO.

## Live session

- The live user `anon` (shell fish, from `/etc/skel`) is created at boot by
  mklive's dracut module.
- `iso/postsetup.sh` writes a live-only greetd config: `initial_session` logs
  `anon` into `dbus-run-session niri --session` once; logging out shows the
  Noctalia greeter. It also resolves the qt6ct `@HOME@` placeholder for
  `/home/anon`, and prints what the greeter's `--setup-system` produced (the
  open palette question from phase 2) into the build log.
- `/etc/sudoers.d/zz-voidbleed-live` gives the live user passwordless sudo.
  It has to sort after `voidbleed-config`'s `wheel` rule, because sudo applies
  the last matching rule and `wheel` asks for a password.
- Boot entry *graphics disabled* (`nomodeset`), or `voidbleed.console` on
  the command line: `iso/include/etc/runit/core-services/90-voidbleed-live.sh`
  keeps greetd down and autologins `anon` on tty1.

## Live-only files (the installer must not carry these over)

- `/etc/runit/core-services/90-voidbleed-live.sh`
- `/etc/sudoers.d/zz-voidbleed-live`
- `/etc/greetd/config.toml` (reinstall `voidbleed-desktop-config`'s version)
- `/etc/skel/.config/qt6ct/qt6ct.conf` with `/home/anon` baked in
- mklive's `/etc/sudoers.d/99-void-live`, `/etc/polkit-1/rules.d/void-live.rules`,
  `/etc/default/live.conf`, `/etc/issue`, `/etc/motd`

## Testing notes

- **niri needs a real GPU.** It refuses software EGL renderers
  ("software EGL renderers are skipped"), so plain `virtio-vga` gives a
  compositor that starts, opens its Wayland socket and never draws; the console
  simply freezes. QEMU therefore needs virgl (`--no-gl` is console-only).
- **QEMU render node.** `qemu-test.sh` picks a non-NVIDIA render node; letting
  virgl pick the NVIDIA node segfaulted QEMU on the reference laptop.
- **Screenshots go through VNC** (`scripts/vnc-shot.py`). QEMU's `screendump`
  fails with "no surface" once the guest scans out through virgl.

## Status

- [x] ISO builds (1.9 GB, 2026-09-15)
- [x] branded GRUB menu, kernel 6.18, no speech entries
- [x] console mode: hostname, issue/motd, autologin, fish + Starship prompt
- [x] graphical session: greetd → niri → Noctalia with red theme (~75 s in QEMU)
- [x] wallpaper boot splash, wallpaper desktop, vibrant generated colors,
      setup wizard skipped (ISO 2026-09-17)
- [ ] rebuild with the live keyring fix and re-verify
- [ ] boots in QEMU legacy BIOS
- [ ] boots on real hardware (reference laptop)

## Live keyring

greetd's `initial_session` logs the live user in without a password, so
`pam_gnome_keyring` cannot create a login keyring and the first app storing a
secret asked "Choose password for new keyring". The live core service now
seeds an unencrypted default keyring in `/home/anon` at boot. Verified in an
isolated session: `secret-tool store` succeeds without a prompter when seeded
and fails without it. Plaintext storage is live-only; installed systems log in
with a password and get a normal encrypted login keyring.
