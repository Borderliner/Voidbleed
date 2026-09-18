#!/usr/bin/env python3
"""Render the images of the Voidbleed Plymouth theme.

    scripts/gen-splash-assets.py

Plymouth's initramfs only carries the theme directory: no fonts, no
fontconfig and no label plugin, so every piece of text on the splash has to
be a picture. This renders them (and the logo, sweep and bullet) into
overlays/system/usr/share/plymouth/themes/voidbleed/, where they are
committed -- Pillow is a developer dependency, not a build one.
"""

import os
import sys

try:
    from PIL import Image, ImageDraw, ImageFont
except ImportError:
    sys.exit("this needs Pillow: xbps-install python3-Pillow")

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
THEME = os.path.join(ROOT, "overlays/system/usr/share/plymouth/themes/voidbleed")
LOGO = os.path.join(ROOT, "overlays/desktop/usr/share/pixmaps/voidbleed-logo.png")
FONT = "/usr/share/fonts/TTF/Ubuntu-R.ttf"

PRIMARY = (232, 49, 63)      # #e8313f
TRACK = (58, 21, 25)         # #3a1519
TEXT = (243, 231, 232)       # #f3e7e8
DIM = (138, 111, 115)        # #8a6f73

# Text is rendered at twice the size it is normally drawn at, so the theme can
# scale it up on tall screens without it turning to mush.
PROMPT_SIZE = 46
SLOGAN_SIZE = 30
SLOGAN_TRACKING = 8  # extra pixels between letters


def save(img, name):
    path = os.path.join(THEME, name)
    img.save(path, optimize=True)
    print(f"{name}: {img.width}x{img.height}, {os.path.getsize(path) // 1024 or 1} KB")


def text_image(text, size, color, tracking=0):
    font = ImageFont.truetype(FONT, size)
    widths = [font.getlength(c) for c in text]
    width = int(sum(widths) + tracking * (len(text) - 1)) + 2 * size
    height = size * 2
    img = Image.new("RGBA", (width, height), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    x = float(size)
    for char, advance in zip(text, widths):
        draw.text((x, height / 2), char, font=font, fill=color + (255,), anchor="lm")
        x += advance + tracking
    return img.crop(img.getbbox())


def main():
    os.makedirs(THEME, exist_ok=True)

    # The 512px logo is far larger than the splash ever draws it, and every
    # byte here is a byte in the initramfs.
    logo = Image.open(LOGO).convert("RGBA")
    save(logo.resize((256, 256), Image.LANCZOS), "logo.png")

    # Solid pixels: the theme scales them to whatever the screen needs.
    save(Image.new("RGBA", (8, 8), TRACK + (255,)), "track.png")

    # The sweep fades in and out of the track instead of ending abruptly.
    sweep = Image.new("RGBA", (192, 8), PRIMARY + (0,))
    for x in range(sweep.width):
        t = x / (sweep.width - 1)
        alpha = (1 - abs(2 * t - 1)) ** 1.6
        for y in range(sweep.height):
            sweep.putpixel((x, y), PRIMARY + (int(255 * alpha),))
    save(sweep, "sweep.png")

    # Passphrase bullet: drawn large and scaled down so the edge stays smooth.
    scale = 8
    dot = Image.new("RGBA", (18 * scale, 18 * scale), (0, 0, 0, 0))
    ImageDraw.Draw(dot).ellipse((0, 0, 18 * scale - 1, 18 * scale - 1), fill=PRIMARY + (255,))
    save(dot.resize((18, 18), Image.LANCZOS), "bullet.png")

    save(text_image("Enter passphrase", PROMPT_SIZE, TEXT), "prompt.png")
    save(text_image("BLEED INTO THE VOID", SLOGAN_SIZE, DIM, SLOGAN_TRACKING), "slogan.png")


if __name__ == "__main__":
    main()
