"""yt-dlp boundary: child environment, progress parsing, process runners."""

from __future__ import annotations

import os
import re
from pathlib import Path


def parse_progress(line: str) -> dict | None:
    match = re.match(
        r"^\[download\]\s+(?P<percent>\d+(?:\.\d+)?)%\s+of\s+~?\s*(?P<size>\S+)"
        r"(?:\s+in\s+(?P<elapsed>\S+))?\s+at\s+(?P<speed>\S+)"
        r"(?:\s+ETA\s+(?P<eta>\S+))?",
        line.strip(),
    )
    if not match:
        return None
    return {
        "percent": float(match.group("percent")),
        "size": match.group("size"),
        "speed": match.group("speed"),
        "eta": match.group("eta"),
    }


def child_env(environ: dict | None = None) -> dict[str, str]:
    """Environment for the yt-dlp child process.

    yt-dlp's launcher resolves its interpreter through `/usr/bin/env python3`.
    Inside a uv script that PATH points at uv's managed CPython, which lacks
    system modules such as secretstorage, so cookie decryption fails. Pruning
    the uv entries restores the interpreter the user's own shell would use.
    """
    env = dict(os.environ if environ is None else environ)
    venv = env.get("VIRTUAL_ENV", "")
    kept = []
    for part in env.get("PATH", "").split(os.pathsep):
        if not part:
            continue
        if venv and part.startswith(venv):
            continue
        if "/uv/" in part or ".local/share/uv" in part:
            continue
        kept.append(part)
    env["PATH"] = os.pathsep.join(kept)
    for key in ("VIRTUAL_ENV", "PYTHONPATH", "PYTHONHOME"):
        env.pop(key, None)
    return env


def detect_merge_format() -> str | None:
    """Best-effort read of --merge-output-format from the user's config."""
    candidates = [
        Path.home() / ".config" / "yt-dlp" / "config",
        Path.home() / ".yt-dlp" / "config",
        Path("/etc/yt-dlp/config"),
    ]
    for path in candidates:
        try:
            text = path.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        match = re.search(r"^\s*--merge-output-format[=\s]+(\w+)", text, re.MULTILINE)
        if match:
            return match.group(1)
    return None

