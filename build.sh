#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

VERSION=${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
OUTDIR=builds

rm -rf "$OUTDIR"
mkdir -p "$OUTDIR"

echo "Building SleepHook-Go $VERSION ..."

GOOS=windows GOARCH=amd64 go build \
    -ldflags="-H windowsgui -s -w -X main.version=$VERSION" \
    -o "$OUTDIR/SleepHook.exe" .

cp config.yaml "$OUTDIR/"
cp resource/sleep-icon.png "$OUTDIR/"

echo "Done → $OUTDIR/"
ls -lh "$OUTDIR/"
