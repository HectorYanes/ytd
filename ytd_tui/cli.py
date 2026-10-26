"""Command line entry point."""

from __future__ import annotations

import argparse
import platform
import sys
from pathlib import Path

from . import __version__
from .app import YtdApp
from .core import (
    config_path,
    detect_merge_format,
    ffmpeg_path,
    js_runtime,
    module_bin_dirs,
    ytdlp_origin,
    ytdlp_version,
)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="ytd", description="Queue TUI for yt-dlp."
    )
    parser.add_argument("urls", nargs="*", help="URLs to queue on start")
    parser.add_argument(
        "-o", "--outdir", default=None, help="download directory (default: ~/Downloads)"
    )
    parser.add_argument(
        "-j", "--jobs", type=int, default=3, help="parallel downloads (default: 3)"
    )
    parser.add_argument("--version", action="version", version=f"ytd {__version__}")
    parser.add_argument(
        "--check",
        action="store_true",
        help="print environment diagnostics and exit (no TUI)",
    )
    return parser


def run_check() -> int:
    """Everything a bug report needs, and nothing that needs a terminal."""
    origin = ytdlp_origin()
    config = config_path()
    ffmpeg = ffmpeg_path()
    runtime = js_runtime(module_bin_dirs())
    outdir = Path.home() / "Downloads"
    lines = [
        f"ytd        {__version__}",
        f"python     {sys.version.split()[0]}",
        f"platform   {platform.system()} {platform.release()} ({platform.machine()})",
        f"yt-dlp     {origin}",
    ]
    if origin != "missing":
        lines.append(f"yt-dlp ver {ytdlp_version()}")
    lines.append(
        f"ffmpeg     {ffmpeg}" if ffmpeg
        else "ffmpeg     MISSING — merging video+audio and embedding will fail"
    )
    lines.append(
        f"js runtime {runtime}" if runtime
        else "js runtime none — YouTube needs deno or node (install yt-dlp[deno])"
    )
    lines.append(f"config     {config or 'none found'}")
    if config:
        lines.append(f"container  {detect_merge_format() or 'not set'}")
    lines.append(f"outdir     {outdir}")
    print("\n".join(lines))
    return 0 if origin != "missing" and ffmpeg else 1


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    if args.check:
        return run_check()
    app = YtdApp(
        outdir=args.outdir, start_urls=args.urls, max_concurrent=max(1, args.jobs)
    )
    app.run()
    return 0


if __name__ == "__main__":
    sys.exit(main())
