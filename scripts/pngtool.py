#!/usr/bin/env python3
"""Minimal PNG read/write/resize, standard library only.

The build hosts have no image tools, so Voidbleed derives its boot splash and
icons from the branding artwork with this.

  pngtool.py fit <src> <dst> <w> <h> [--darken F] [--bg RRGGBB]
      Scale src to fit w*h (box filter, aspect preserved), centre it on bg,
      optionally multiply brightness by F.
  pngtool.py trim <src> <dst> [--size N]
      Crop fully transparent edges, then pad to a square and scale to N.
"""

import struct
import sys
import zlib

def paeth(a, b, c):
    """PNG Paeth predictor: ties prefer a, then b (plain min() gets this wrong)."""
    pa, pb, pc = abs(b - c), abs(a - c), abs(a + b - 2 * c)
    if pa <= pb and pa <= pc:
        return a
    return b if pb <= pc else c


def read_png(path):
    """Return (width, height, rows-of-RGBA-bytes) for 8-bit RGB/RGBA PNGs."""
    data = open(path, "rb").read()
    if data[:8] != b"\x89PNG\r\n\x1a\n":
        raise SystemExit(f"{path}: not a PNG")
    pos, idat, width = 8, b"", None
    while pos < len(data):
        length, tag = struct.unpack(">I4s", data[pos:pos + 8])
        chunk = data[pos + 8:pos + 8 + length]
        if tag == b"IHDR":
            width, height, depth, color, _, _, interlace = struct.unpack(">IIBBBBB", chunk)
            if depth != 8 or color not in (2, 6) or interlace:
                raise SystemExit(f"{path}: need 8-bit RGB/RGBA, non-interlaced")
        elif tag == b"IDAT":
            idat += chunk
        elif tag == b"IEND":
            break
        pos += 12 + length
    if width is None:
        raise SystemExit(f"{path}: no IHDR")

    channels = 3 if color == 2 else 4
    raw = zlib.decompress(idat)
    stride = width * channels
    rows, prev, pos = [], bytearray(stride), 0
    for _ in range(height):
        filt = raw[pos]
        line = bytearray(raw[pos + 1:pos + 1 + stride])
        pos += 1 + stride
        if filt == 1:
            for i in range(channels, stride):
                line[i] = (line[i] + line[i - channels]) & 0xFF
        elif filt == 2:
            for i in range(stride):
                line[i] = (line[i] + prev[i]) & 0xFF
        elif filt == 3:
            for i in range(stride):
                left = line[i - channels] if i >= channels else 0
                line[i] = (line[i] + ((left + prev[i]) >> 1)) & 0xFF
        elif filt == 4:
            for i in range(stride):
                left = line[i - channels] if i >= channels else 0
                upleft = prev[i - channels] if i >= channels else 0
                line[i] = (line[i] + paeth(left, prev[i], upleft)) & 0xFF
        elif filt != 0:
            raise SystemExit(f"{path}: bad filter {filt}")
        rows.append(line)
        prev = line
    if channels == 3:
        rows = [bytearray(b"".join(bytes(r[i * 3:i * 3 + 3]) + b"\xff" for i in range(width)))
                for r in rows]
    return width, height, rows


def write_png(path, width, height, rows, alpha=False):
    channels = 4 if alpha else 3
    if not alpha:
        rows = [bytearray(b"".join(bytes(r[i * 4:i * 4 + 3]) for i in range(width))) for r in rows]
    raw = b"".join(b"\x00" + bytes(r) for r in rows)
    chunk = lambda t, d: struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d))
    open(path, "wb").write(
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 6 if alpha else 2, 0, 0, 0))
        + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b""))


def resize(width, height, rows, new_w, new_h):
    """Box-filter resize over RGBA rows."""
    out = []
    for y in range(new_h):
        y0, y1 = y * height // new_h, max(y * height // new_h + 1, (y + 1) * height // new_h)
        line = bytearray(new_w * 4)
        for x in range(new_w):
            x0, x1 = x * width // new_w, max(x * width // new_w + 1, (x + 1) * width // new_w)
            r = g = b = a = n = 0
            for sy in range(y0, y1):
                row = rows[sy]
                for sx in range(x0, x1):
                    o = sx * 4
                    r += row[o]; g += row[o + 1]; b += row[o + 2]; a += row[o + 3]
                    n += 1
            o = x * 4
            line[o:o + 4] = bytes((r // n, g // n, b // n, a // n))
        out.append(line)
    return out


def fit(src, dst, w, h, darken=1.0, bg=(0x0E, 0x09, 0x0A)):
    sw, sh, rows = read_png(src)
    scale = min(w / sw, h / sh)
    tw, th = max(1, round(sw * scale)), max(1, round(sh * scale))
    scaled = resize(sw, sh, rows, tw, th)
    ox, oy = (w - tw) // 2, (h - th) // 2
    canvas = [bytearray(bytes(bg + (255,)) * w) for _ in range(h)]
    for y in range(th):
        row, dstrow = scaled[y], canvas[y + oy]
        for x in range(tw):
            o, d = x * 4, (x + ox) * 4
            dstrow[d] = min(255, int(row[o] * darken))
            dstrow[d + 1] = min(255, int(row[o + 1] * darken))
            dstrow[d + 2] = min(255, int(row[o + 2] * darken))
    write_png(dst, w, h, canvas)
    print(f"{dst} ({w}x{h}) from {src} ({sw}x{sh})")


def trim(src, dst, size=None):
    sw, sh, rows = read_png(src)
    top, bottom, left, right = sh, 0, sw, 0
    for y in range(sh):
        row = rows[y]
        for x in range(sw):
            if row[x * 4 + 3] > 8:
                top, bottom = min(top, y), max(bottom, y)
                left, right = min(left, x), max(right, x)
    if bottom < top:
        raise SystemExit(f"{src}: fully transparent")
    cw, ch = right - left + 1, bottom - top + 1
    side = max(cw, ch)
    canvas = [bytearray(side * 4) for _ in range(side)]
    ox, oy = (side - cw) // 2, (side - ch) // 2
    for y in range(ch):
        src_row, dst_row = rows[top + y], canvas[y + oy]
        dst_row[ox * 4:(ox + cw) * 4] = src_row[left * 4:(right + 1) * 4]
    if size and size != side:
        canvas = resize(side, side, canvas, size, size)
        side = size
    write_png(dst, side, side, canvas, alpha=True)
    print(f"{dst} ({side}x{side}) trimmed from {src} ({sw}x{sh})")


def main():
    args = sys.argv[1:]
    if len(args) >= 5 and args[0] == "fit":
        darken, bg = 1.0, (0x0E, 0x09, 0x0A)
        if "--darken" in args:
            darken = float(args[args.index("--darken") + 1])
        if "--bg" in args:
            v = int(args[args.index("--bg") + 1], 16)
            bg = (v >> 16 & 255, v >> 8 & 255, v & 255)
        fit(args[1], args[2], int(args[3]), int(args[4]), darken, bg)
        return 0
    if len(args) >= 3 and args[0] == "trim":
        size = int(args[args.index("--size") + 1]) if "--size" in args else None
        trim(args[1], args[2], size)
        return 0
    print(__doc__.strip())
    return 2


if __name__ == "__main__":
    sys.exit(main())
