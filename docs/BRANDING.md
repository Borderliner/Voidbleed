# Voidbleed branding

- **Name:** Voidbleed (`voidbleed`, package prefix `voidbleed-`)
- **Slogan:** Bleed into the void.
- **Primary color:** `#e8313f`

## Assets

| File | Installed to | Used by |
|---|---|---|
| `overlays/desktop/usr/share/backgrounds/voidbleed/voidbleed1.png` | `/usr/share/backgrounds/voidbleed/` | desktop wallpaper, greeter wallpaper, boot splash source |
| `overlays/desktop/usr/share/pixmaps/voidbleed-logo.png` | `/usr/share/pixmaps/` | `LOGO=voidbleed-logo` in os-release, 512x512 with alpha |

The boot splash is generated from the wallpaper at ISO build time
(`scripts/pngtool.py fit … 640 480 --darken 0.72`), so boot and desktop match
and no image is committed twice.

## Colors

Noctalia generates the palette from the wallpaper using the **vibrant** scheme
(`source = "wallpaper"` in the shipped config), so the shell picks up the
artwork's reds. The fixed palette below still ships as
`palettes/Voidbleed.json` for anyone who prefers constant colors
(`source = "custom"`), and it seeds the login screen before a user theme syncs.

## Palette

The source of truth is `overlays/base/etc/skel/.config/noctalia/palettes/Voidbleed.json`.
Every other place that hard-codes colors (niri placeholders, Ghostty theme seed,
Starship seed, greeter seed) copies from it, and Noctalia overwrites those
generated files once the session starts.

| Role | Dark | Light |
|---|---|---|
| primary | `#e8313f` | `#b3172b` |
| on primary | `#140709` | `#ffffff` |
| secondary | `#ff8a85` | `#9b3a3a` |
| tertiary (ember) | `#f0a35e` | `#9a4d12` |
| error / urgent (amber) | `#ffc247` | `#8a5a00` |
| surface | `#0e090a` | `#fcf5f5` |
| surface variant | `#211416` | `#f1dfe0` |
| on surface | `#f3e7e8` | `#1f1214` |
| outline | `#5e3a3f` | `#a08286` |
| hover | `#b01f2d` | `#f6d2d6` |

Error is amber rather than red so urgent states stay distinguishable from the
brand color. All text/background pairs meet WCAG AA (4.5:1); primary on
surface is 4.65:1 in dark mode.

## Assets still to make (phase 7)

Logo (`voidbleed-logo`), wallpapers in `/usr/share/backgrounds/voidbleed`,
GRUB theme, installer ASCII logo.
