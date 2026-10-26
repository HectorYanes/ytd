#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = [
#   "textual>=1.0",
#   "yt-dlp[default,deno]>=2024.1.1",
#   "yt-dlp[secretstorage]>=2024.1.1; sys_platform == 'linux'",
# ]
# ///
"""No-install launcher: `uv run ytd.py`, `./ytd` or `ytd.cmd` on Windows.

Installing the package (`uv tool install .`) is the recommended path; this
script exists so a single unzipped folder works without installing anything
beyond uv itself.
"""

from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from ytd_tui.cli import main  # noqa: E402

if __name__ == "__main__":
    sys.exit(main())
