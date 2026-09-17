# Phase 1 — capture and clean up the reference machine

Reference: the author's Void laptop (Intel UHD 630 + Quadro P3200, niri 26.04,
noctalia 5.1.0, noctalia-greeter 1.3.0), snapshot taken 2026-09-16.

## What changed compared to the reference machine

| Area | Reference machine | Voidbleed |
|---|---|---|
| Terminal | foot | Ghostty (Noctalia `ghostty` template enabled) |
| Theme | wallpaper-generated green palette | custom `Voidbleed` red palette |
| Icons | Slot-Beauty-Dark (manual install, unpackaged) | Papirus-Dark |
| Telemetry | Noctalia telemetry on | off |
| Prompt | starship installed but never initialised | initialised in fish |
| `Mod+P` mirror bind | used `jq`, which wasn't installed | `jq` added to base |
| Screencasting | no GNOME portal (niri needs it for screen sharing) | `xdg-desktop-portal-gnome` added |
| Monitors / keyboard | `eDP-1`/`VGA-1` blocks, `us,ir` | removed; installer writes `local.kdl` |
| Power | TLP and PPD both enabled (they conflict) | PPD by default, TLP as an alternative |
| Bluetooth audio | bluez-alsa + PipeWire | PipeWire only |
| NVIDIA, Intel VA-API, microcode, Flatpak apps, CUPS | always installed | chosen in the installer (`optional.toml`) |
| sshd, fancontrol | enabled | off (sshd optional, fancontrol manual) |

Personal items left out entirely: fish PATH entries for bun/opencode/mise,
Throne, opencode, 9router autostart, Noctalia location, wallpaper paths,
lockscreen widget geometry, the Claude URL handler in mimeapps.

## Noctalia facts this relies on (verified on noctalia 5.1.0)

- There is **no system-wide config**: `/etc/xdg` and `XDG_CONFIG_DIRS` are not
  read. Defaults must be in `/etc/skel/.config/noctalia/`.
- Custom palettes live in `~/.config/noctalia/palettes/<Name>.json`.
- `~/.local/state/noctalia/settings.toml` (Settings window) overrides `config.toml`.
- The niri template only needs `include "noctalia.kdl"` in `config.kdl`; niri
  fails on a missing non-optional include, so skel ships a pre-rendered one.
- The Starship hook replaces only the block between its marker comments.

## Requirements handed to the installer (phase 4)

0. Before the first xbps transaction into the target, copy
   `overlays/system/usr/share/xbps.d/05-voidbleed-*.conf` into the target's
   `usr/share/xbps.d`. Otherwise base-system pulls Void's `linux`
   metapackage and base-files extracts Void's os-release before
   `voidbleed-config` is there to prevent it.
1. After `useradd -m`, replace `@HOME@` in `~/.config/qt6ct/qt6ct.conf`.
2. Write `~/.config/niri/local.kdl` with the chosen keyboard layouts.
3. Add the user to `wheel` (the 0440 sudoers drop-in ships in `voidbleed-config`).
   Plain `useradd -m` already gives fish as the shell once `voidbleed-config`
   is installed; don't pass `-s` unless the user picks another shell.
4. Install `voidbleed-desktop` plus everything in `catalog/defaults.txt`.
5. Enable `catalog/services.txt` plus services of selected groups; never enable
   `dhcpcd` or `wpa_supplicant` alongside NetworkManager.
6. For groups with `flatpaks`, add the Flathub remote system-wide first.
7. For groups with `cmdline`, append to `GRUB_CMDLINE_LINUX_DEFAULT` before
   generating grub.cfg; rebuild the initramfs after installing NVIDIA.
8. Create the user's XDG dirs (`xdg-user-dirs-update`) so screenshots have a home.

## Decisions

- **sudo** is the privilege tool; opendoas is not shipped.
- **vpm** is part of the base.
- **noctalia-greeter** and **gnome-keyring** are hard dependencies of `voidbleed-desktop`.
  gnome-keyring unlocks at login through greetd's stock PAM file, and niri's
  portal config routes the Secret portal to it.

## Open decisions

- **Ghostty version.** Void ships 1.1.3. Noctalia's live-reload hook targets
  newer Ghostty, so theme changes may apply only to new windows.

os-release branding and the Noctalia package source were settled in
[phase 2](phase-2.md).
