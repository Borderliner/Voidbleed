#!/usr/bin/env bash
# Export the Voidbleed repository public key as the plist XBPS stores in
# /var/db/xbps/keys, into packages/keys/.
#
# XBPS only verifies signatures of remote repositories, so build/repo is served
# over a temporary localhost HTTP server and synced into a scratch root, which
# imports the key.

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo="$root/build/repo"
port="${PORT:-8765}"
tmp="$(mktemp -d)"

cleanup() {
    [ -n "${server:-}" ] && kill "$server" 2>/dev/null || true
    rm -rf "$tmp"
}
trap cleanup EXIT

[ -e "$repo/x86_64-repodata" ] || { echo "error: $repo has no repodata" >&2; exit 1; }

python3 -m http.server --bind 127.0.0.1 --directory "$repo" "$port" >/dev/null 2>&1 &
server=$!
sleep 1

mkdir -p "$tmp/root/var/db/xbps/keys"
xbps-install -r "$tmp/root" -S --repository="http://127.0.0.1:$port" >/dev/null < <(yes)

mkdir -p "$root/packages/keys"
found=0
for k in "$tmp/root/var/db/xbps/keys"/*.plist; do
    [ -e "$k" ] || continue
    cp "$k" "$root/packages/keys/"
    echo "exported $(basename "$k")"
    found=1
done
[ "$found" = 1 ] || { echo "error: no key was imported" >&2; exit 1; }
