# Voidbleed

> Bleed into the void.

A Void Linux variant (glibc, x86_64) for laptops and desktops: niri, Noctalia v5,
Ghostty, and a Bubbletea TUI installer.

## Layout

| Path | What it is |
|---|---|
| `catalog/base.txt`, `catalog/desktop.txt` | Hard dependencies of `voidbleed-base` / `voidbleed-desktop` |
| `catalog/defaults.txt` | Installed on every system but removable (Thunar, btop, …) |
| `catalog/optional.toml` | Drivers and apps picked in the installer (NVIDIA, Flatpak apps, …) |
| `catalog/services.txt` | runit services enabled on every install |
| `catalog/excluded*.txt` | What was left out from the reference machine, and why |
| `overlays/system/` | Files shipped by `voidbleed-config` (branding, repo, sudoers) |
| `overlays/desktop/` | Files shipped by `voidbleed-desktop-config` (skel configs, greetd, PipeWire) |
| `overlays/nvidia/` | Files shipped by `voidbleed-nvidia-config` |
| `packages/srcpkgs/` | xbps-src templates for the `voidbleed-*` packages |
| `packages/fallback/` | Unbuilt templates to rehost noctalia/greeter/adw-gtk-theme if voiders disappears |
| `packages/config.env` | Repo URL, homepage, maintainer, signing key path |
| `packages/keys/` | Repository public key (as installed into `/var/db/xbps/keys`) |
| `iso/` | Live ISO definition: groups, live-only files, postsetup hook, splash |
| `scripts/` | Snapshot, catalog checks, package/ISO builds, repo tests, QEMU helpers |
| `scripts/pngtool.py` | Stdlib-only PNG resize/trim (splash and icons from the artwork) |
| `docs/` | Branding and per-phase notes |

## Common tasks

```sh
scripts/catalog.py validate            # catalog + templates consistent, packages exist
scripts/catalog.py sync-templates      # after editing base.txt or desktop.txt
scripts/build-packages.sh              # build everything into build/repo (signed)
scripts/build-packages.sh voidbleed-desktop-config   # rebuild one package
scripts/test-repo.sh                   # install-test the repo in a scratch root
sudo scripts/build-iso.sh [--fast]     # live ISO into build/ (see docs/phase-3.md)
scripts/qemu-test.sh                   # boot the newest ISO in QEMU
scripts/snapshot-host.sh && scripts/catalog.py drift snapshot/<name>
```
