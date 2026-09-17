#!/usr/bin/env python3
"""End-to-end install test in QEMU.

Boots the live ISO with a blank virtual disk, logs in as root on a text
console, runs the installer unattended with a config file, then reboots the
VM from the installed disk and takes screenshots.

The installer binary and catalog are served from the host, so a freshly built
installer can be tested against an existing ISO.

Usage:
  scripts/vm-install-test.py --config install.toml [--iso ISO] [--out DIR]
                             [--installer PATH] [--passphrase TEXT]
                             [--password TEXT] [--keep] [--diagnose]
  scripts/vm-install-test.py --reuse build/vm-work-XXXX [--password TEXT] [--diagnose]

  --keep      keep the VM disk and firmware variables (printed at the end)
  --reuse     skip the install and boot a kept disk again
  --diagnose  after logging in, collect process, DRM and kernel state from
              tty2 into the output directory
"""

import argparse
import http.server
import io
import os
import shutil
import socket
import subprocess
import sys
import tarfile
import tempfile
import threading
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OVMF_CODE = "/usr/share/edk2/x64/OVMF_CODE.4m.fd"
OVMF_VARS = "/usr/share/edk2/x64/OVMF_VARS.4m.fd"
HOST_IN_GUEST = "10.0.2.2"

KEYS = {" ": "spc", "-": "minus", "_": "shift-minus", "/": "slash", ".": "dot", ",": "comma",
        "=": "equal", "|": "shift-backslash", ">": "shift-dot", "<": "shift-comma", ":": "shift-semicolon",
        ";": "semicolon", "'": "apostrophe", '"': "shift-apostrophe", "\n": "ret", "&": "shift-7",
        "$": "shift-4", "!": "shift-1", "@": "shift-2", "#": "shift-3", "%": "shift-5", "*": "shift-8",
        "(": "shift-9", ")": "shift-0", "+": "shift-equal", "?": "shift-slash", "~": "shift-grave_accent",
        "{": "shift-bracket_left", "}": "shift-bracket_right", "[": "bracket_left", "]": "bracket_right",
        "\\": "backslash", "^": "shift-6", "`": "grave_accent"}


def log(msg):
    print(time.strftime("%H:%M:%S"), msg, flush=True)


def render_node():
    """First non-NVIDIA render node; QEMU's virgl crashes on NVIDIA here."""
    for name in sorted(os.listdir("/sys/class/drm")):
        if not name.startswith("renderD"):
            continue
        try:
            vendor = open(f"/sys/class/drm/{name}/device/vendor").read().strip()
        except OSError:
            continue
        if vendor != "0x10de":
            return "/dev/dri/" + name
    return None


def free_port():
    with socket.socket() as s:
        s.bind(("0.0.0.0", 0))
        return s.getsockname()[1]


class Server:
    """Serves files to the guest and receives uploads."""

    def __init__(self, files, out, port=0):
        self.files, self.out = files, out
        self.port = port or free_port()
        server = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                data = server.files.get(self.path.lstrip("/"))
                if data is None:
                    self.send_response(404)
                    self.end_headers()
                    return
                self.send_response(200)
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            def do_POST(self):
                name = os.path.basename(self.path)
                body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
                with open(os.path.join(server.out, name), "wb") as f:
                    f.write(body)
                log(f"received {name} ({len(body)} bytes)")
                self.send_response(200)
                self.end_headers()

            def log_message(self, *args):
                pass

        self.httpd = http.server.ThreadingHTTPServer(("0.0.0.0", self.port), Handler)
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()


class VM:
    def __init__(self, args, workdir, name, cdrom):
        self.sock = os.path.join(workdir, f"{name}.monitor")
        self.vnc = 20 + (os.getpid() % 40)
        cmd = [
            "qemu-system-x86_64", "-name", "voidbleed-test",
            "-machine", "q35,accel=kvm", "-cpu", "host", "-smp", "4", "-m", "4G",
            "-drive", f"if=pflash,format=raw,readonly=on,file={OVMF_CODE}",
            "-drive", f"if=pflash,format=raw,file={args.vars}",
            "-drive", f"file={args.disk},if=none,format=raw,id=disk",
            "-device", "virtio-blk-pci,drive=disk,bootindex=1",
            "-nic", "user,model=virtio-net-pci",
            "-device", "qemu-xhci", "-device", "usb-tablet",
            "-device", "virtio-vga-gl",
            "-display", "egl-headless" + (f",rendernode={render_node()}" if render_node() else ""),
            "-vnc", f"127.0.0.1:{self.vnc}",
            "-monitor", f"unix:{self.sock},server,nowait",
        ]
        if cdrom:
            # bootindex, not -boot order: the firmware's saved Voidbleed entry
            # would otherwise win once a system is installed.
            cmd += ["-drive", f"file={args.iso},if=none,media=cdrom,readonly=on,id=cd",
                    "-device", "ide-cd,drive=cd,bootindex=0"]
        self.proc = subprocess.Popen(cmd, stdout=open(os.path.join(args.out, f"qemu-{name}.log"), "wb"),
                                     stderr=subprocess.STDOUT)
        for _ in range(100):
            if os.path.exists(self.sock):
                break
            time.sleep(0.1)
        self.mon = socket.socket(socket.AF_UNIX)
        self.mon.connect(self.sock)

    def hmp(self, cmd, settle=0.05):
        self.mon.sendall((cmd + "\n").encode())
        time.sleep(settle)
        self.mon.settimeout(0.05)
        try:
            while self.mon.recv(65536):
                pass
        except (socket.timeout, BlockingIOError):
            pass

    def key(self, combo, settle=0.4):
        self.hmp("sendkey " + combo, settle)

    def type(self, text):
        for ch in text:
            if ch in KEYS:
                k = KEYS[ch]
            elif ch.isdigit() or ch.islower():
                k = ch
            elif ch.isupper():
                k = "shift-" + ch.lower()
            else:
                raise SystemExit(f"no key for {ch!r}")
            self.hmp("sendkey " + k, 0.04)

    def shot(self, path):
        subprocess.run([sys.executable, os.path.join(ROOT, "scripts", "vnc-shot.py"),
                        f"127.0.0.1:{5900 + self.vnc}", path], check=False)

    def wait_exit(self, timeout):
        try:
            self.proc.wait(timeout=timeout)
            return True
        except subprocess.TimeoutExpired:
            return False

    def stop(self):
        if self.proc.poll() is None:
            self.hmp("quit", 0.5)
            try:
                self.proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.proc.kill()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--config")
    ap.add_argument("--reuse", help="boot a kept work directory without installing")
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--diagnose", action="store_true")
    ap.add_argument("--port", type=int, default=0, help="fixed port for the host HTTP server")
    ap.add_argument("--live-script", help="with --reuse: boot the ISO with the kept disk attached, run this "
                    "shell script as root in the live system, then power off")
    ap.add_argument("--wait", type=int, default=45, help="seconds to wait after logging in")
    ap.add_argument("--keys", default="", help="comma-separated keys to send after logging in "
                    "(a repaint helps QEMU refresh the screen after the greeter hands over)")
    ap.add_argument("--iso")
    ap.add_argument("--installer", default=os.path.join(ROOT, "build", "voidbleed-installer"))
    ap.add_argument("--out", default=os.path.join(ROOT, "build", "vm-test"))
    ap.add_argument("--passphrase", help="LUKS passphrase to type at boot")
    ap.add_argument("--password", help="user password to type into the greeter")
    ap.add_argument("--root-password", help="root password for --diagnose (root is locked by default)")
    ap.add_argument("--install-timeout", type=int, default=2400)
    args = ap.parse_args()

    if not args.iso:
        isos = sorted((os.path.join(ROOT, "build", f) for f in os.listdir(os.path.join(ROOT, "build"))
                       if f.startswith("voidbleed-live-") and f.endswith(".iso")), key=os.path.getmtime)
        if not isos:
            raise SystemExit("no ISO in build/; build one or pass --iso")
        args.iso = isos[-1]
    shutil.rmtree(args.out, ignore_errors=True)
    os.makedirs(args.out)
    if args.reuse:
        workdir = os.path.abspath(args.reuse)
    else:
        if not args.config and not args.live_script:
            raise SystemExit("--config is required unless --reuse or --live-script is given")
        # Not /tmp: it is often tmpfs, and the disk image receives a whole install.
        os.makedirs(os.path.join(ROOT, "build"), exist_ok=True)
        workdir = tempfile.mkdtemp(prefix="vm-work-", dir=os.path.join(ROOT, "build"))
    args.disk = os.path.join(workdir, "disk.img")
    args.vars = os.path.join(workdir, "vars.fd")
    if not args.reuse:
        with open(args.disk, "wb") as f:  # sparse raw image; no qemu-img needed
            f.truncate(32 << 30)
        shutil.copy(OVMF_VARS, args.vars)

    catalog = io.BytesIO()
    with tarfile.open(fileobj=catalog, mode="w") as tar:
        tar.add(os.path.join(ROOT, "catalog"), arcname="catalog")
    files = {"catalog.tar": catalog.getvalue()}
    # The live image carries these; stage them so an ISO without them can still
    # be used to test network installs and live-group removal.
    repo_dir = os.path.join(ROOT, "build", "repo")
    if os.path.isdir(repo_dir):
        repo_tar = io.BytesIO()
        with tarfile.open(fileobj=repo_tar, mode="w") as tar:
            tar.add(repo_dir, arcname="repo")
        files["repo.tar"] = repo_tar.getvalue()
    live_groups = os.path.join(ROOT, "iso", "live-groups.txt")
    if os.path.exists(live_groups):
        files["live-groups.txt"] = open(live_groups, "rb").read()
    if not args.reuse:
        files["voidbleed-installer"] = open(args.installer, "rb").read()
        files["install.toml"] = open(args.config, "rb").read()
    server = Server(files, args.out, args.port)
    base = f"http://{HOST_IN_GUEST}:{server.port}"
    files["run.sh"] = f"""\
exec >/tmp/run.out 2>&1
set -x
cd /tmp
mkdir -p /usr/share/voidbleed
curl -fsS {base}/repo.tar | tar -x -C /usr/share/voidbleed || true
curl -fsS -o /usr/share/voidbleed/live-groups.txt {base}/live-groups.txt || true
curl -fsS -o vbi {base}/voidbleed-installer && chmod +x vbi
curl -fsS {base}/catalog.tar | tar -x -C /tmp
curl -fsS -o install.toml {base}/install.toml
lsblk
./vbi --config install.toml --catalog /tmp/catalog --yes > install.out 2>&1
echo "installer exit=$?" >> install.out
curl -fsS --data-binary @install.out {base}/upload/install.out
curl -fsS --data-binary @/var/log/voidbleed-install.log {base}/upload/voidbleed-install.log
curl -fsS --data-binary @/tmp/run.out {base}/upload/run.out
poweroff
""".encode()

    if args.live_script:
        files["run.sh"] = (f"exec >/tmp/run.out 2>&1\nset -x\nBASE={base}\n" +
                           open(args.live_script).read() +
                           f"\ncurl -fsS --data-binary @/tmp/run.out {base}/upload/run.out\npoweroff\n").encode()
        run_live(args, workdir, base)
        return
    if args.reuse:
        boot_installed(args, workdir, base)
        return

    log(f"ISO {os.path.basename(args.iso)}, serving on port {server.port}")
    run_live(args, workdir, base)

    result = open(os.path.join(args.out, "install.out")).read() if os.path.exists(os.path.join(args.out, "install.out")) else ""
    print(result)
    if "installer exit=0" not in result:
        raise SystemExit("install failed; see " + args.out)

    try:
        boot_installed(args, workdir, base)
    finally:
        if args.keep:
            log("kept VM disk: " + os.path.relpath(workdir, os.getcwd()))
        else:
            shutil.rmtree(workdir, ignore_errors=True)


def run_live(args, workdir, base):
    """Boot the ISO, run run.sh from the host as root on tty3, wait for poweroff."""
    vm = VM(args, workdir, "live", cdrom=True)
    try:
        log("booting the live ISO")
        time.sleep(12)
        vm.key("ret")
        time.sleep(95)
        vm.shot(os.path.join(args.out, "1-live-desktop.png"))
        vm.key("ctrl-alt-f3", 3)
        vm.type("root\n")
        time.sleep(3)
        vm.type("voidbleed\n")
        time.sleep(6)
        vm.type(f"curl -fsS {base}/run.sh | sh\n")
        log("live script running")
        if not vm.wait_exit(args.install_timeout):
            vm.shot(os.path.join(args.out, "2-live-timeout.png"))
            raise SystemExit("live script did not finish in time")
        log("live VM powered off")
    finally:
        vm.stop()


def boot_installed(args, workdir, base):
    log("booting the installed disk")
    vm = VM(args, workdir, "installed", cdrom=False)
    try:
        time.sleep(20)
        vm.shot(os.path.join(args.out, "3-boot.png"))
        if args.passphrase:
            vm.type(args.passphrase + "\n")
        time.sleep(70)
        vm.shot(os.path.join(args.out, "4-greeter.png"))
        if args.password:
            vm.type(args.password + "\n")
            time.sleep(args.wait)
            for combo in filter(None, args.keys.split(",")):
                vm.key(combo, 1.5)
            vm.hmp("mouse_move 40 40")
            time.sleep(3)
            vm.shot(os.path.join(args.out, "5-desktop.png"))
        if args.diagnose:
            diagnose(vm, args, base)
    finally:
        vm.stop()
    log("done: " + args.out)


def diagnose(vm, args, base):
    """Log in on tty2 and upload what the graphical session is doing."""
    vm.key("ctrl-alt-f2", 3)
    vm.type("root\n")
    time.sleep(3)
    if args.root_password:
        vm.type(args.root_password + "\n")
        time.sleep(5)
    script = (
        "{ echo == ps; ps -eo pid,user,tty,stat,wchan:20,cmd; "
        "echo == vt; fgconsole; cat /sys/class/tty/tty0/active; "
        "echo == dri; ls -l /dev/dri; "
        "echo == greetd; cat /etc/greetd/config.toml; "
        "echo == dmesg; dmesg | tail -n 60; } > /tmp/diag.txt 2>&1; "
        f"curl -fsS --data-binary @/tmp/diag.txt {base}/upload/diag.txt\n"
    )
    vm.type(script)
    time.sleep(8)
    vm.shot(os.path.join(args.out, "6-diagnose.png"))


if __name__ == "__main__":
    main()
