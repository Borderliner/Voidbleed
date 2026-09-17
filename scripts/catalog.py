#!/usr/bin/env python3
"""Check the Voidbleed package catalog.

  catalog.py validate                 catalog is well formed, packages exist, templates in sync
  catalog.py sync-templates [--check] write metapackage depends from base.txt / desktop.txt
  catalog.py drift <snapshot>         compare a host snapshot against the catalog
  catalog.py list <file>              print the package names in a catalog list
  catalog.py group <field> <id>...    print a field (packages, services, …) of optional groups
  catalog.py kernel                   print the pinned kernel series package

Needs only the Python standard library and xbps-query.
"""

import json
import re
import subprocess
import sys
import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CATALOG = ROOT / "catalog"
SRCPKGS = ROOT / "packages/srcpkgs"
PALETTE = ROOT / "overlays/desktop/etc/skel/.config/noctalia/palettes/Voidbleed.json"

# Metapackage -> (catalog list, extra Voidbleed packages it pulls in).
METAPACKAGES = {
    "voidbleed-base": ("base.txt", ["voidbleed-config"]),
    "voidbleed-desktop": ("desktop.txt", ["voidbleed-base", "voidbleed-desktop-config"]),
}
TIERS = ("base.txt", "desktop.txt", "defaults.txt")

GROUP_KEYS = {
    "id", "name", "description", "category", "default", "detect", "exclusive",
    "requires", "repos", "packages", "flatpaks", "services", "overlay", "cmdline",
}
REQUIRED_KEYS = {"id", "name", "description", "category", "default"}
DETECT_PREFIXES = ("gpu:", "cpu:", "chassis:", "bluetooth")
PALETTE_ROLES = {
    "mPrimary", "mOnPrimary", "mSecondary", "mOnSecondary", "mTertiary", "mOnTertiary",
    "mError", "mOnError", "mSurface", "mOnSurface", "mSurfaceVariant", "mOnSurfaceVariant",
    "mOutline", "mShadow", "mHover", "mOnHover",
}
# Oldest kernel series Voidbleed may pin. Newer series (7.x included) are allowed.
MIN_KERNEL = (6, 18)
KERNEL_SERIES = re.compile(r"^linux(\d+)\.(\d+)$")
KERNEL_IGNORE = ROOT / "overlays/system/usr/share/xbps.d/05-voidbleed-kernel.conf"
DEPENDS_BLOCK = re.compile(r"(# BEGIN generated[^\n]*\n)(.*?)(# END generated)", re.S)


def read_list(path):
    """Package or service names from a one-per-line file; tab-separated notes are ignored."""
    names = []
    for line in path.read_text().splitlines():
        line = line.split("#", 1)[0].split("\t", 1)[0].strip()
        if line:
            names.append(line)
    return names


def load_optional():
    with open(CATALOG / "optional.toml", "rb") as f:
        return tomllib.load(f)


def local_packages():
    """Packages built from packages/srcpkgs, which live in the Voidbleed repo."""
    return {p.name for p in SRCPKGS.iterdir() if (p / "template").is_file()}


def kernel_series():
    """The pinned kernel package from base.txt, e.g. 'linux6.18'."""
    series = [p for p in read_list(CATALOG / "base.txt") if KERNEL_SERIES.match(p)]
    return series[0] if len(series) == 1 else None


def check_kernel(tiers, groups):
    errors = []
    pinned = [p for pkgs in tiers.values() for p in pkgs if KERNEL_SERIES.match(p)]
    if len(pinned) != 1 or pinned[0] not in tiers["base.txt"]:
        return [f"kernel: exactly one linuxX.Y must be pinned in base.txt, found {pinned}"]
    series = pinned[0]
    major, minor = map(int, KERNEL_SERIES.match(series).groups())
    if (major, minor) < MIN_KERNEL:
        errors.append(f"kernel: {series} is older than linux{MIN_KERNEL[0]}.{MIN_KERNEL[1]}")
    for meta in ("linux", "linux-lts", "linux-mainline"):
        if any(meta in pkgs for pkgs in tiers.values()):
            errors.append(f"kernel: {meta} metapackage must not be listed; pin a series instead")
    for g in groups:
        for p in g.get("packages", []):
            if p == "linux-headers" or (p.endswith("-headers") and p.startswith("linux") and p != f"{series}-headers"):
                errors.append(f"{g['id']}: {p} does not match the pinned kernel ({series}-headers)")
    ignored = {l.split("=", 1)[1].strip() for l in KERNEL_IGNORE.read_text().splitlines()
               if l.startswith("ignorepkg=")}
    if not {"linux", "linux-headers"} <= ignored:
        errors.append(f"kernel: {KERNEL_IGNORE.name} must ignore linux and linux-headers")
    return errors


def repo_has(pkg):
    return subprocess.run(
        ["xbps-query", "-R", "-p", "pkgver", pkg],
        capture_output=True,
    ).returncode == 0


def render_depends(pkgs):
    return 'depends="\n' + "".join(f" {p}\n" for p in pkgs) + '"\n'


def sync_templates(check=False):
    stale = []
    for name, (listfile, extra) in METAPACKAGES.items():
        path = SRCPKGS / name / "template"
        text = path.read_text()
        if not DEPENDS_BLOCK.search(text):
            print(f"  ✗ {path}: missing BEGIN/END generated markers")
            return 1
        body = render_depends(extra + read_list(CATALOG / listfile))
        new = DEPENDS_BLOCK.sub(lambda m: m.group(1) + body + m.group(3), text)
        if new != text:
            stale.append(name)
            if not check:
                path.write_text(new)
    if check and stale:
        print(f"  ✗ out of date: {', '.join(stale)} (run scripts/catalog.py sync-templates)")
        return 1
    print(f"  ✓ metapackage templates {'in sync' if check else 'written'}"
          + (f" ({', '.join(stale)} updated)" if stale and not check else ""))
    return 0


def validate():
    errors = []
    tiers = {t: read_list(CATALOG / t) for t in TIERS}
    services = read_list(CATALOG / "services.txt")
    opt = load_optional()
    groups = opt.get("group", [])
    local = local_packages()

    seen = {}
    for listname, pkgs in tiers.items():
        for p in pkgs:
            if p in seen:
                errors.append(f"{p}: listed in both {seen[p]} and {listname}")
            seen[p] = listname

    ids = [g.get("id") for g in groups]
    for i in sorted({i for i in ids if ids.count(i) > 1}):
        errors.append(f"optional.toml: duplicate group id {i!r}")

    categories = set(opt.get("categories", []))
    exclusive = {}
    for g in groups:
        gid = g.get("id", "<missing id>")
        if missing := REQUIRED_KEYS - g.keys():
            errors.append(f"{gid}: missing keys {sorted(missing)}")
        if unknown := g.keys() - GROUP_KEYS:
            errors.append(f"{gid}: unknown keys {sorted(unknown)}")
        if g.get("category") not in categories:
            errors.append(f"{gid}: category {g.get('category')!r} not in categories")
        if g.get("default") not in ("on", "off", "auto"):
            errors.append(f"{gid}: default must be on/off/auto")
        if g.get("default") == "auto" and not g.get("detect"):
            errors.append(f"{gid}: default is auto but detect is empty")
        for d in g.get("detect", []):
            if not d.startswith(DETECT_PREFIXES):
                errors.append(f"{gid}: unknown detect condition {d!r}")
        for r in g.get("requires", []):
            if r not in ids:
                errors.append(f"{gid}: requires unknown group {r!r}")
        if ov := g.get("overlay"):
            if not (ROOT / ov).is_dir():
                errors.append(f"{gid}: overlay {ov} does not exist")
        for p in g.get("packages", []):
            if p in seen:
                errors.append(f"{gid}: {p} is already in {seen[p]}")
        if g.get("exclusive"):
            exclusive.setdefault(g["exclusive"], []).append(g)

    for key, members in exclusive.items():
        if sum(1 for m in members if m.get("default") == "on") > 1:
            errors.append(f"exclusive {key!r}: more than one group defaults to on")

    everything = set(seen)
    for g in groups:
        everything.update(g.get("packages", []))
    remote = sorted(everything - local)
    print(f"checking {len(remote)} packages against the configured repositories "
          f"({len(everything & local)} built by Voidbleed)...")
    for p in remote:
        if not repo_has(p):
            errors.append(f"{p}: not found in any repository or packages/srcpkgs")

    errors.extend(check_kernel(tiers, groups))

    for s in services:
        if not s.replace("-", "").replace("_", "").isalnum():
            errors.append(f"services.txt: odd service name {s!r}")

    palette = json.loads(PALETTE.read_text())
    if "dark" not in palette:
        errors.append("Voidbleed.json: missing dark variant")
    for mode in ("dark", "light"):
        if mode in palette and (missing := PALETTE_ROLES - palette[mode].keys()):
            errors.append(f"Voidbleed.json {mode}: missing roles {sorted(missing)}")

    if errors:
        print("\n".join(f"  ✗ {e}" for e in errors))
        print(f"{len(errors)} problem(s)")
        return 1
    print("  ✓ " + ", ".join(f"{len(v)} {k.removesuffix('.txt')}" for k, v in tiers.items())
          + f", {len(groups)} optional groups, {len(services)} services")
    return sync_templates(check=True)


def drift(snapshot):
    snap = Path(snapshot)
    host = set(read_list(snap / "meta/packages-manual.txt"))
    tiers = set()
    for t in TIERS:
        tiers.update(read_list(CATALOG / t))
    excluded = set(read_list(CATALOG / "excluded.txt"))
    optional = set()
    for g in load_optional().get("group", []):
        optional.update(g.get("packages", []))
    known = tiers | excluded | optional

    untracked = sorted(host - known)
    not_on_host = sorted(tiers - host)

    host_services = set(read_list(snap / "meta/services.txt"))
    catalog_services = set(read_list(CATALOG / "services.txt"))
    catalog_services.update(read_list(CATALOG / "excluded-services.txt"))
    for g in load_optional().get("group", []):
        catalog_services.update(g.get("services", []))
    extra_services = sorted(
        s for s in host_services - catalog_services if not s.startswith("agetty-")
    )

    def section(title, items, hint):
        print(f"\n{title} ({len(items)})")
        print(f"  {hint}")
        for i in items:
            print(f"   - {i}")

    section("Installed on host but not in the catalog", untracked,
            "add to a tier list, optional.toml, or excluded.txt with a reason")
    section("In the catalog but not explicitly installed on host", not_on_host,
            "fine if pulled in as a dependency or newly chosen for Voidbleed")
    section("Enabled on host but not managed by the catalog", extra_services,
            "add to services.txt, an optional group, or excluded-services.txt")
    return 1 if untracked else 0


def main():
    args = sys.argv[1:]
    if args == ["validate"]:
        return validate()
    if args and args[0] == "sync-templates" and args[1:] in ([], ["--check"]):
        return sync_templates(check=args[1:] == ["--check"])
    if len(args) == 2 and args[0] == "list":
        print("\n".join(read_list(CATALOG / args[1])))
        return 0
    if args == ["kernel"]:
        series = kernel_series()
        if not series:
            print("no single linuxX.Y pinned in base.txt", file=sys.stderr)
            return 1
        print(series)
        return 0
    if len(args) >= 3 and args[0] == "group":
        groups = {g["id"]: g for g in load_optional().get("group", [])}
        missing = [i for i in args[2:] if i not in groups]
        if missing:
            print(f"unknown group(s): {', '.join(missing)}", file=sys.stderr)
            return 1
        for i in args[2:]:
            for value in groups[i].get(args[1], []):
                print(value)
        return 0
    if len(args) == 2 and args[0] == "drift":
        return drift(args[1])
    print(__doc__.strip())
    return 2


if __name__ == "__main__":
    sys.exit(main())
