# Phase 4 — installer engine

One Go module at the repository root, no C dependencies, built statically.

```
internal/config    what to install; TOML for unattended installs
internal/hw        disks, GPUs, CPU, chassis, firmware, memory
internal/catalog   reads catalog/, resolves optional groups for the hardware
internal/sys       the only way the installer changes the machine
internal/engine    plan + ordered steps + progress events
internal/tui       the interface (phase 5)
cmd/voidbleed-installer
```

## Choices

- **UEFI only.** A 1 GiB ESP is mounted at `/boot`, so kernels and the
  initramfs stay readable to GRUB. That matters with encryption: GRUB can't
  unlock a LUKS2 volume that uses the default argon2id key derivation, and a
  separate unencrypted `/boot` partition is avoided this way.
- **ext4 by default, btrfs optional** with `@`, `@home`, `@snapshots`,
  `@var_log` subvolumes mounted with zstd compression.
- **LUKS2** on the root partition, opened as `voidbleed-root`; dracut gets the
  `crypt` module, `/etc/crypttab` and `rd.luks.uuid` on the kernel command line.
- **Swap**: none, zram (default, via zramen), a swap file (its own
  uncompressed subvolume on btrfs), or a partition. A swap partition is refused
  with encryption, because it would sit outside the encrypted volume.
- **Both sources.** Offline copies the running live system with
  `tar --one-file-system` (what void-installer does); network installs current
  packages with xbps. Either way the target's xbps rules and repository keys
  are seeded first, so the kernel pin and os-release branding apply from the
  first package on.

## Safety

- Preflight refuses BIOS boot, a missing disk, the disk the live system runs
  from, disks with mounted partitions, and disks below 16 GiB. Nothing touches
  the disk until it passes.
- Any failure unmounts the target and closes the LUKS volume, in that order.
- Passwords and passphrases are only ever passed on stdin, never as arguments,
  so they can't appear in logs, transcripts or `ps`.
- Every install writes `/var/log/voidbleed-install.log`, copied into the new
  system before unmounting.

## Offline installs

The live image is a running system, so the copy has to be cleaned up: the live
user is deleted before `/proc` is mounted in the chroot (`userdel` refuses
users with running processes otherwise), the machine id and entropy seed are
regenerated, the live sudo/polkit/getty tweaks are removed, and greetd's
autologin config and the Qt skeleton are restored from what the packages ship.
Optional software baked into the image but not selected is removed; anything
selected that the image lacks is downloaded.

## Tests

- `go test ./...` — unit tests for every package.
- Golden transcripts (`internal/engine/testdata`) record every command, file
  write and symlink for three layouts. `-update` rewrites them.
- `scripts/vm-install-test.py` installs for real in QEMU and boots the result.

Verified in QEMU on 2026-09-17, against the 2026-09-17 ISO:

| Case | Result |
|---|---|
| offline, ext4, zram, flatpak + firefox | installed in 2m01s, boots to the greeter and desktop |
| network, btrfs + LUKS2, 2 GiB swap file | installed in 6m24s, asks for the passphrase at boot |

The VM test found a real bug this way: the installed system's session started
without a D-Bus session bus and hung before drawing anything (see
`fix(desktop): start the session with its own dbus bus`). Both cases boot to
the desktop with that fix.
