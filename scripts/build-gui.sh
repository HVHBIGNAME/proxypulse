#!/usr/bin/env bash
# Build the Windows GUI binary, embedding the icon and the Common Controls v6
# manifest (walk needs the manifest or the window fails with TTM_ADDTOOL).
set -euo pipefail
cd "$(dirname "$0")/.."

echo "[1/3] generating icon..."
python scripts/make_icon.py

if ! command -v windres >/dev/null 2>&1; then
  echo "error: windres not found. Install mingw-w64 (MSYS2: pacman -S mingw-w64-x86_64-binutils)" >&2
  exit 1
fi

echo "[2/3] compiling resources..."
(cd assets && windres -i proxypulse.rc \
    -o ../cmd/proxypulse-gui/rsrc.syso \
    --input-format=rc --output-format=coff)

echo "[3/3] building GUI..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "-H windowsgui -s -w" -o proxypulse-gui.exe ./cmd/proxypulse-gui

echo "done: proxypulse-gui.exe"
