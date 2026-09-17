#!/usr/bin/env python3
"""Grab a PNG screenshot from a VNC server (QEMU's -vnc).

QEMU's HMP `screendump` fails with "no surface" when the guest scans out through
virgl, so GL screenshots go through VNC instead.

Usage: vnc-shot.py <host:port> <out.png>
"""

import socket
import struct
import sys
import time
import zlib


def recv_exact(sock, n):
    buf = b""
    while len(buf) < n:
        chunk = sock.recv(n - len(buf))
        if not chunk:
            raise EOFError("connection closed")
        buf += chunk
    return buf


def connect(host, port):
    sock = socket.create_connection((host, port), timeout=15)
    sock.settimeout(20)
    version = recv_exact(sock, 12)
    if not version.startswith(b"RFB "):
        raise SystemExit(f"not a VNC server: {version!r}")
    sock.sendall(b"RFB 003.008\n")

    count = recv_exact(sock, 1)[0]
    if count == 0:
        reason_len = struct.unpack(">I", recv_exact(sock, 4))[0]
        raise SystemExit("server refused: " + recv_exact(sock, reason_len).decode())
    types = recv_exact(sock, count)
    if 1 not in types:
        raise SystemExit(f"server wants authentication (types {list(types)})")
    sock.sendall(b"\x01")
    if struct.unpack(">I", recv_exact(sock, 4))[0] != 0:
        raise SystemExit("security handshake failed")

    sock.sendall(b"\x01")  # ClientInit, shared
    width, height = struct.unpack(">HH", recv_exact(sock, 4))
    recv_exact(sock, 16)  # server pixel format, replaced below
    name_len = struct.unpack(">I", recv_exact(sock, 4))[0]
    recv_exact(sock, name_len)

    # 32bpp true colour, little-endian BGRX.
    sock.sendall(b"\x00\x00\x00\x00" + struct.pack(
        ">BBBBHHHBBBxxx", 32, 24, 0, 1, 255, 255, 255, 16, 8, 0))
    sock.sendall(struct.pack(">BxHi", 2, 1, 0))  # SetEncodings: raw
    return sock, width, height


def capture(sock, width, height):
    """One full framebuffer update as a list of rows of RGB bytes."""
    sock.sendall(struct.pack(">BBHHHH", 3, 0, 0, 0, width, height))
    while True:
        msg = recv_exact(sock, 1)[0]
        if msg == 0:
            break
        if msg == 1:  # SetColourMapEntries
            _, _, n = struct.unpack(">BHH", recv_exact(sock, 5))
            recv_exact(sock, n * 6)
        elif msg == 2:  # Bell
            pass
        elif msg == 3:  # ServerCutText
            recv_exact(sock, 3)
            n = struct.unpack(">I", recv_exact(sock, 4))[0]
            recv_exact(sock, n)
        else:
            raise SystemExit(f"unexpected server message {msg}")

    recv_exact(sock, 1)
    rects = struct.unpack(">H", recv_exact(sock, 2))[0]
    rows = [bytearray(b"\x00" * (width * 3)) for _ in range(height)]
    for _ in range(rects):
        x, y, w, h, enc = struct.unpack(">HHHHi", recv_exact(sock, 12))
        if enc != 0:
            raise SystemExit(f"unsupported encoding {enc}")
        data = recv_exact(sock, w * h * 4)
        for row in range(h):
            if not 0 <= y + row < height:
                continue
            src = data[row * w * 4:(row + 1) * w * 4]
            dst = rows[y + row]
            for col in range(w):
                if 0 <= x + col < width:
                    b, g, r = src[col * 4], src[col * 4 + 1], src[col * 4 + 2]
                    off = (x + col) * 3
                    dst[off:off + 3] = bytes((r, g, b))
    return rows


def write_png(path, width, height, rows):
    raw = b"".join(b"\x00" + bytes(row) for row in rows)
    chunk = lambda t, d: struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d))
    with open(path, "wb") as f:
        f.write(b"\x89PNG\r\n\x1a\n"
                + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0))
                + chunk(b"IDAT", zlib.compress(raw, 6))
                + chunk(b"IEND", b""))


def main():
    if len(sys.argv) != 3:
        print(__doc__.strip())
        return 2
    host, _, port = sys.argv[1].rpartition(":")
    sock, width, height = connect(host or "127.0.0.1", int(port))
    try:
        capture(sock, width, height)      # first update primes the framebuffer
        time.sleep(0.5)
        rows = capture(sock, width, height)
    finally:
        sock.close()
    write_png(sys.argv[2], width, height, rows)
    print(f"{sys.argv[2]} ({width}x{height})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
