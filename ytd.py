#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = ["textual>=1.0"]
# ///
"""ytd — a queue TUI for yt-dlp.

Paste a URL (or several), get a row per URL with the quality and audio that
will be downloaded and its live status. Pick specific formats from the
explorer table, or let the yt-dlp config decide.

Usage:  ytd [URL ...]
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from app import YtdApp  # noqa: E402


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="ytd", description="Queue TUI for yt-dlp."
    )
    parser.add_argument("urls", nargs="*", help="URLs to queue on start")
    parser.add_argument(
        "-o", "--outdir", default=None, help="download directory (default: ~/Downloads)"
    )
    parser.add_argument(
        "-j",
        "--jobs",
        type=int,
        default=3,
        help="parallel downloads (default: 3)",
    )
    args = parser.parse_args(argv)
    app = YtdApp(
        outdir=args.outdir, start_urls=args.urls, max_concurrent=max(1, args.jobs)
    )
    app.run()
    return 0


if __name__ == "__main__":
    sys.exit(main())
