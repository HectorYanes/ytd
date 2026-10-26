#!/bin/sh
# Install ytd on Linux or macOS: uv, ffmpeg, then the tool itself.
# ASCII only: this file is also read by shells with a non-UTF-8 locale.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

OS=$(uname -s)
case $OS in
    Linux | Darwin) ;;
    *) die "unsupported system '$OS'; on Windows run install.ps1 instead" ;;
esac

say "== ytd installer ($OS)"

# ---- 1. uv -----------------------------------------------------------------
if ! command -v uv >/dev/null 2>&1; then
    say "-- uv not found, installing the official one"
    curl -LsSf https://astral.sh/uv/install.sh | sh ||
        die "could not download the uv installer"
    for dir in "$HOME/.local/bin" "$HOME/.cargo/bin"; do
        [ -d "$dir" ] && PATH="$dir:$PATH"
    done
    export PATH
    command -v uv >/dev/null 2>&1 ||
        die "uv was installed but is not on PATH yet; open a new terminal and rerun"
fi
say "-- uv $(uv --version | cut -d' ' -f2)"

# ---- 2. ffmpeg -------------------------------------------------------------
if ! command -v ffmpeg >/dev/null 2>&1; then
    say "-- ffmpeg not found, installing it"
    SUDO=
    if [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1; then
        SUDO=sudo
    fi
    if [ "$OS" = Darwin ]; then
        command -v brew >/dev/null 2>&1 ||
            die "install Homebrew (https://brew.sh) or ffmpeg, then rerun"
        brew install ffmpeg
    elif command -v apt-get >/dev/null 2>&1; then
        $SUDO apt-get update
        $SUDO apt-get install -y ffmpeg
    elif command -v dnf >/dev/null 2>&1; then
        $SUDO dnf install -y ffmpeg
    elif command -v pacman >/dev/null 2>&1; then
        $SUDO pacman -S --needed --noconfirm ffmpeg
    elif command -v zypper >/dev/null 2>&1; then
        $SUDO zypper --non-interactive install ffmpeg
    else
        die "install ffmpeg with your package manager, then rerun"
    fi
    command -v ffmpeg >/dev/null 2>&1 ||
        die "ffmpeg is still missing after the install attempt"
fi
say "-- ffmpeg $(ffmpeg -version 2>/dev/null | head -n 1 | cut -d' ' -f3)"

# ---- 3. what to install ----------------------------------------------------
# Prefer a built wheel next to this script; fall back to the source tree.
target=$(ls -t "$ROOT"/dist/ytd_tui-*.whl 2>/dev/null | head -n 1) || true
[ -n "${target:-}" ] || target=$ROOT
say "-- package: $target"

BIN=$(uv tool dir --bin)

# A foreign ytd in the tool bin directory would be silently replaced.
if [ -e "$BIN/ytd" ] && ! uv tool list 2>/dev/null | grep -q '^ytd-tui '; then
    if [ "${YTD_ALLOW_OVERWRITE:-}" = 1 ]; then
        say "-- replacing the existing $BIN/ytd"
    else
        die "$BIN/ytd already exists and is not managed by uv. Remove it with 'rm \"$BIN/ytd\"' and rerun, or set YTD_ALLOW_OVERWRITE=1."
    fi
fi

# ---- 4. install ------------------------------------------------------------
say "-- installing the tool"
uv tool install --force "$target"

# Put the tool bin directory on PATH for future shells. Skipped when the
# caller redirected it (UV_TOOL_BIN_DIR), e.g. for a throwaway test install.
if [ -z "${UV_TOOL_BIN_DIR:-}" ]; then
    uv tool update-shell >/dev/null 2>&1 || true
fi

# ---- 5. verify -------------------------------------------------------------
YTD="$BIN/ytd"
[ -x "$YTD" ] || die "install finished but $YTD is missing"
case "$("$YTD" --version 2>/dev/null || true)" in
    "ytd "* | "ytd "*) ;;
    *) die "the installed entry point did not run: $YTD --version" ;;
esac
say ""
"$YTD" --version
set +e
"$YTD" --check
check_rc=$?
set -e
if [ "$check_rc" -eq 0 ]; then
    say ""
    say "ready: run 'ytd' to open the queue."
else
    say ""
    say "installed, but --check reported missing pieces above; fix those and"
    say "run 'ytd --check' again before using it."
fi
