#!/usr/bin/env bash
# Build ytd from source and put the binary on PATH.
#
#   ./install.sh            → ~/.local/bin/ytd
#   PREFIX=/usr/local ./install.sh
#
# Needs Go 1.26+ only for this build. The installed binary needs nothing but
# ffmpeg; it fetches yt-dlp and deno itself on first use.
set -euo pipefail

cd "$(dirname "$0")"

PREFIX="${PREFIX:-$HOME/.local}"
BINDIR="$PREFIX/bin"
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"

if ! command -v go >/dev/null 2>&1; then
  echo "ytd: Go is required to build from source (https://go.dev/dl/)." >&2
  echo "Prebuilt binaries: https://github.com/<owner>/ytd/releases" >&2
  exit 1
fi

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux|darwin) ;;
  *) echo "ytd: unsupported OS '$os' — use the Windows build." >&2; exit 1 ;;
esac

echo "ytd: building $VERSION for $os/$(uname -m)…"
mkdir -p "$BINDIR"
go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$BINDIR/ytd" ./cmd/ytd

if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "ytd: ffmpeg is missing. Install it (apt install ffmpeg / brew install ffmpeg);"
  echo "     merging separate video+audio streams will fail without it."
fi

echo "ytd: installed $BINDIR/ytd"
case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *) echo "ytd: add this to your shell profile:  export PATH=\"$BINDIR:\$PATH\"" ;;
esac

"$BINDIR/ytd" --update || true
"$BINDIR/ytd" --check || true
