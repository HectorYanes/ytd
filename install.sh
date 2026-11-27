#!/usr/bin/env bash
# Build ytd from source and put the binary on PATH.
#
#   ./install.sh            → ~/.local/bin/ytd
#   PREFIX=/usr/local ./install.sh
#
# Needs Go 1.26.5+ only for this build. The installed binary needs nothing but
# ffmpeg; it fetches yt-dlp and deno itself on first use.
set -euo pipefail

repo_dir=""
tmp_dir=""
tmp_binary=""

cleanup() {
  if [[ -n "$tmp_binary" ]]; then
    rm -f -- "$tmp_binary"
  fi
  if [[ -n "$tmp_dir" ]]; then
    rm -rf -- "$tmp_dir"
  fi
}
trap cleanup EXIT

PREFIX="${PREFIX:-$HOME/.local}"
BINDIR="$PREFIX/bin"

if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
  repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
fi

if ! command -v go >/dev/null 2>&1; then
  echo "ytd: Go 1.26.5+ is required to build from source (https://go.dev/dl/)." >&2
  exit 1
fi

if [[ -z "$repo_dir" || ! -f "$repo_dir/go.mod" ]]; then
  if ! command -v curl >/dev/null 2>&1 || ! command -v tar >/dev/null 2>&1; then
    echo "ytd: curl and tar are required to download the source archive." >&2
    exit 1
  fi
  tmp_dir="$(mktemp -d)"
  echo "ytd: downloading source from GitHub..."
  curl -fsSL --retry 3 \
    "https://codeload.github.com/HectorYanes/ytd/tar.gz/refs/heads/main" \
    | tar -xz -C "$tmp_dir"
  repo_dir="$tmp_dir/ytd-main"
  if [[ ! -f "$repo_dir/go.mod" ]]; then
    echo "ytd: downloaded source archive did not contain go.mod." >&2
    exit 1
  fi
fi

cd "$repo_dir"
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux|darwin) ;;
  *) echo "ytd: unsupported OS '$os' — use the Windows build." >&2; exit 1 ;;
esac

echo "ytd: building $VERSION for $os/$(uname -m)…"
mkdir -p "$BINDIR"
tmp_binary="$(mktemp "$BINDIR/.ytd.XXXXXXXX")"
go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$tmp_binary" ./cmd/ytd
chmod 755 "$tmp_binary"
mv -f -- "$tmp_binary" "$BINDIR/ytd"
tmp_binary=""

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
