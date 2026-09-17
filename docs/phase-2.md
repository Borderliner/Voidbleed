# Phase 2 — packages

Voidbleed's own packages are built locally into a signed repository in
`build/repo`. Nothing is hosted yet (`VOIDBLEED_REPO_URL` is empty); phase 3
puts this repo on the ISO.

## Packages

| Package | Kind | Contents |
|---|---|---|
| `voidbleed-base` | meta | `catalog/base.txt` + `voidbleed-config` |
| `voidbleed-desktop` | meta | `catalog/desktop.txt` + `voidbleed-base` + `voidbleed-desktop-config` |
| `voidbleed-config` | files | `overlays/system`: os-release, noextract rule, kernel pin, sudoers, voiders repo entry, repo keys, vpm fish completion; sets fish as the `useradd` default shell |
| `voidbleed-desktop-config` | files | `overlays/desktop`: skel configs (incl. `~/.local/bin`), wallpaper, logo, greetd config, greeter seed, greeter sync polkit rule, fish SSH-agent hook, PipeWire links |
| `voidbleed-nvidia-config` | files | `overlays/nvidia`: modprobe options, niri VRAM heap profile |

The metapackages' `depends` are generated from the catalog
(`scripts/catalog.py sync-templates`); `validate` fails if they drift.
Removable defaults (`catalog/defaults.txt`) are not metapackage dependencies;
the installer installs them explicitly.

## Where third-party packages come from

- **Official Void repos:** everything except the three below.
- **Voiders Community Repository** (`https://repo.voiders.dev`): noctalia,
  noctalia-greeter, adw-gtk-theme. `voidbleed-config` ships the repo entry
  and its key (`a8:f0:05:df:01:c4:37:92:83:f6:8b:9a:ce:ab:73:29`, the
  fingerprint published in their README), so installs never prompt.
- `packages/fallback/` keeps MIT-licensed templates to rebuild those three
  ourselves if needed (built successfully once on 2026-09-16).

## How the tricky parts work

- **os-release.** base-files owns `/usr/lib/os-release`. `voidbleed-config`
  installs `noextract=/usr/lib/os-release` and its INSTALL script writes the
  Voidbleed file; later base-files updates skip extracting it. Tested with a
  forced base-files reinstall.
- **Kernel.** Voidbleed pins a kernel series (`linux6.18` + `linux-base` in
  `catalog/base.txt`) rather than Void's `linux` metapackage, which
  base-system depends on. `05-voidbleed-kernel.conf` ignores `linux` and
  `linux-headers`; NVIDIA groups use `linux6.18-headers`. `catalog.py validate`
  enforces one pinned series ≥ 6.18, matching headers, and no kernel
  metapackages. 7.x is not blocked: `xbps-install linux7.2` or
  `linux-mainline` still work; bump the pin in base.txt deliberately.
  Tested: with the config present, `voidbleed-base` resolves `linux6.18` and
  no `linux` metapackage.
- **greetd.** greetd owns `/etc/greetd/config.toml` as a conf file, so
  `voidbleed-desktop-config` rewrites it from INSTALL (keeping a
  `.voidbleed-backup`) only if it doesn't already launch the Noctalia greeter.
- **Login screen theme.** The INSTALL script runs
  `noctalia-greeter-apply-appearance --setup-system`, then seeds `sync.toml`
  with the Voidbleed palette if it has none. Whether `--setup-system` writes a
  palette of its own is unverified (it needs real root); check in phase 3.
- **Greeter theme sync.** Noctalia's `shell.greeter_sync` runs
  `noctalia-greeter-apply-appearance` through polkit, whose action defaults to
  `auth_admin`. `49-noctalia-greeter-sync.rules` (in `/usr/share/polkit-1/rules.d`)
  allows it for active, local `wheel` users so syncing never prompts.
- **Shell.** fish is the login shell for every account `useradd` creates:
  `voidbleed-config`'s INSTALL changes `/etc/default/useradd` (a shadow conf
  file) from `SHELL=/bin/bash` to `/usr/bin/fish`, only when it still holds
  Void's default. root keeps bash. `vpm` ships only a bash completion, so
  Voidbleed adds `/usr/share/fish/vendor_completions.d/vpm.fish` for all users.
  From the reference fish config, only `~/.local/bin` is generic; `.bun`,
  `.opencode`, `mise` and the `fishcfg` alias are personal.
- **gnome-keyring.** No packaging needed: greetd's stock PAM file already has
  `pam_gnome_keyring`, and niri's portal config routes Secret to it. The niri
  config starts its SSH component; `voidbleed-ssh-agent.fish` (vendor_conf.d)
  points `SSH_AUTH_SOCK` at `$XDG_RUNTIME_DIR/keyring/ssh` when unset.
- **Signing.** `scripts/build-packages.sh` signs repodata and every package
  with `VOIDBLEED_SIGNING_KEY` (default
  `~/.local/share/voidbleed/keys/repo-signing.pem`, outside the repo). The
  public key is exported once into `packages/keys/` and shipped in
  `voidbleed-config`.

## Verification (`scripts/test-repo.sh`)

Run without root, in a user namespace against a scratch root:

- repo signature verifies with the shipped key
- `voidbleed-desktop` + defaults resolve (585 packages)
- config packages install; INSTALL results checked (os-release, greetd,
  greeter seed, sudoers 0440, skel, PipeWire links, `_greeter` account)
- branding survives a base-files reinstall

Not yet verified: a full install booting to the greeter — that is phase 3.

## Rebuilding

Bump `version`/`revision` in a template for changes that installed systems
should pick up. During development the script force-rebuilds `voidbleed-*`
regardless.
