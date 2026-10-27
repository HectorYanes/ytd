"""yt-dlp boundary: child environment, progress parsing, process runners."""

from __future__ import annotations

import asyncio
import importlib.util
import json
import os
import platform
import re
import shutil
import subprocess
import sys
from pathlib import Path

from .model import Status


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


def _prune_path(value: str, sep: str, venv: str = "") -> str:
    """Drop PATH entries that point at uv's managed Python or a venv.

    Works with both `:` and `;` separators, so the Windows layout is testable
    from Linux.
    """
    kept: list[str] = []
    venv_norm = venv.replace("\\", "/").lower().rstrip("/")
    for part in value.split(sep):
        if not part.strip():
            continue
        norm = part.replace("\\", "/").lower().rstrip("/")
        if venv_norm and (norm == venv_norm or norm.startswith(venv_norm + "/")):
            continue
        if "/uv/python" in norm or "uv\\python" in part.lower():
            continue
        if "/share/uv/" in norm or "/uv/tools/" in norm:
            continue
        kept.append(part)
    return sep.join(kept)


def child_env(
    environ: dict | None = None, extra_path: list[str] | None = None
) -> dict[str, str]:
    """Environment for the yt-dlp child process.

    yt-dlp's own launcher resolves its interpreter through `/usr/bin/env
    python3` (or a `.exe` shim that does the same). Inside a uv script or tool
    environment that PATH points at uv's managed CPython, which lacks system
    modules such as secretstorage, so cookie decryption fails. Pruning the uv
    entries restores the interpreter the user's own shell would use. UTF-8 is
    forced so progress text and titles decode the same on every OS.
    """
    env = dict(os.environ if environ is None else environ)
    sep = ";" if os.name == "nt" else os.pathsep
    path = _prune_path(env.get("PATH", ""), sep, env.get("VIRTUAL_ENV", ""))
    extra = [str(entry) for entry in (extra_path or []) if entry]
    env["PATH"] = sep.join([*extra, path]) if extra else path
    for key in ("VIRTUAL_ENV", "PYTHONPATH", "PYTHONHOME"):
        env.pop(key, None)
    env["PYTHONUTF8"] = "1"
    env["PYTHONIOENCODING"] = "utf-8"
    return env


def config_candidates(
    system: str | None = None,
    home: Path | None = None,
    environ: dict | None = None,
) -> list[Path]:
    """Config file locations yt-dlp itself checks, most specific first."""
    system = system or platform.system()
    home = home or Path.home()
    env = dict(os.environ if environ is None else environ)
    out: list[Path] = []
    if system == "Windows":
        appdata = env.get("APPDATA")
        if appdata:
            base = Path(appdata) / "yt-dlp"
            out += [base / "config", base / "config.txt"]
        out.append(home / "yt-dlp.conf")
    elif system == "Darwin":
        base = home / "Library" / "Application Support" / "yt-dlp"
        out += [base / "config", base / "config.txt", home / "yt-dlp.conf"]
    else:
        xdg = env.get("XDG_CONFIG_HOME") or str(home / ".config")
        out += [
            Path(xdg) / "yt-dlp" / "config",
            Path(xdg) / "yt-dlp" / "config.txt",
            home / "yt-dlp.conf",
            Path("/etc/yt-dlp.conf"),
            Path("/etc/yt-dlp/config"),
        ]
    return out


def config_path(
    system: str | None = None,
    home: Path | None = None,
    environ: dict | None = None,
) -> Path | None:
    for path in config_candidates(system, home, environ):
        try:
            if path.is_file():
                return path
        except OSError:
            continue
    return None


def parse_merge_format(text: str) -> str | None:
    for flag in ("merge-output-format", "remux-video"):
        match = re.search(rf"^\s*--{flag}[=\s]+(\w+)", text, re.MULTILINE)
        if match:
            return match.group(1).lower()
    return None


def detect_merge_format(
    system: str | None = None,
    home: Path | None = None,
    environ: dict | None = None,
) -> str | None:
    """Best-effort read of the merge container from the user's yt-dlp config."""
    path = config_path(system, home, environ)
    if path is None:
        return None
    try:
        text = path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return None
    return parse_merge_format(text)


# --------------------------------------------------------------------------- #
# analysis
# --------------------------------------------------------------------------- #
class YtdlpError(RuntimeError):
    """Raised when yt-dlp cannot produce usable JSON."""


def ytdlp_prefix() -> list[str]:
    """How to invoke yt-dlp, in priority order.

    1. ``YTD_YTDLP`` — an explicit path the user pinned.
    2. ``yt-dlp`` on PATH — the user's own install, with their config and
       extras (this is what Linux/macOS users already have).
    3. the bundled ``yt-dlp`` module — the Windows case, where the package
       dependency is the only copy around.
    """
    override = os.environ.get("YTD_YTDLP", "").strip()
    if override:
        return [override]
    found = shutil.which("yt-dlp")
    if found:
        return [found]
    if importlib.util.find_spec("yt_dlp") is not None:
        return [sys.executable, "-m", "yt_dlp"]
    raise YtdlpError(
        "yt-dlp not found: install it (pip install -U yt-dlp) or set YTD_YTDLP"
    )


def module_bin_dirs(prefix: list[str] | None = None) -> list[str]:
    """Extra PATH entries needed when we run the bundled module.

    The module lives in our own environment, so its ``bin``/``Scripts``
    directory is where the bundled ``deno`` (JS runtime) and ``ffmpeg`` shims
    are — and that directory is not on the user's PATH when a console script
    is launched.
    """
    chosen = prefix or ytdlp_prefix()
    if len(chosen) > 1 and chosen[1] == "-m":
        return [str(Path(sys.executable).parent)]
    return []


def js_runtime(extra_path: list[str] | None = None) -> str | None:
    """Name of a JavaScript runtime yt-dlp can use for YouTube, if any."""
    sep = ";" if os.name == "nt" else ":"
    search = [*(extra_path or []), os.environ.get("PATH", "")]
    for name in ("deno", "node", "bun", "qjs"):
        if shutil.which(name):
            return name
        for directory in sep.join(search).split(sep):
            if not directory:
                continue
            for suffix in ("", ".exe"):
                candidate = Path(directory) / f"{name}{suffix}"
                try:
                    if candidate.is_file() and os.access(candidate, os.X_OK):
                        return str(candidate)
                except OSError:
                    continue
    return None


def ytdlp_origin() -> str:
    """Human description of the resolution above, for `--check`."""
    override = os.environ.get("YTD_YTDLP", "").strip()
    if override:
        return f"YTD_YTDLP={override}"
    found = shutil.which("yt-dlp")
    if found:
        return f"PATH ({found})"
    if importlib.util.find_spec("yt_dlp") is not None:
        return f"bundled module ({sys.executable} -m yt_dlp)"
    return "missing"


def ytdlp_version(prefix: list[str] | None = None) -> str:
    try:
        result = subprocess.run(
            [*(prefix or ytdlp_prefix()), "--version"],
            capture_output=True,
            text=True,
            timeout=20,
            env=child_env(extra_path=module_bin_dirs(prefix or ytdlp_prefix())),
        )
    except (OSError, subprocess.SubprocessError):
        return "unknown"
    return (result.stdout or result.stderr).strip().splitlines()[0] if result.stdout or result.stderr else "unknown"


def ffmpeg_path() -> str | None:
    return shutil.which("ffmpeg")


def analyze_command(
    url: str, *, flat: bool = False, prefix: list[str] | None = None
) -> list[str]:
    cmd = [*(prefix or ytdlp_prefix()), "-J", "--no-warnings"]
    if flat:
        cmd.append("--flat-playlist")
    cmd += ["--no-playlist", url]
    return cmd


def decode_json(payload: str, err_text: str, code: int) -> dict:
    """Parse yt-dlp stdout; raise a useful error instead of returning junk."""
    tail = " ".join(err_text.splitlines()[-3:])
    if not payload.strip():
        raise YtdlpError(tail or f"yt-dlp exited with {code}")
    try:
        info = json.loads(payload)
    except json.JSONDecodeError as exc:
        raise YtdlpError("could not parse yt-dlp JSON output") from exc
    if not isinstance(info, dict):
        raise YtdlpError(tail or f"yt-dlp produced no info (exit {code})")
    return info


async def run_json(
    url: str, *, flat: bool = False, timeout: float = 180
) -> tuple[dict, str]:
    """Return (info, warnings). A non-zero exit with usable JSON is tolerated."""
    prefix = ytdlp_prefix()
    cmd = analyze_command(url, flat=flat, prefix=prefix)
    try:
        proc = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            env=child_env(extra_path=module_bin_dirs(prefix)),
        )
    except FileNotFoundError as exc:
        raise YtdlpError("yt-dlp not found in PATH") from exc
    try:
        out, err = await asyncio.wait_for(proc.communicate(), timeout=timeout)
    except asyncio.TimeoutError as exc:
        proc.kill()
        raise YtdlpError(f"yt-dlp timed out after {timeout:.0f}s") from exc
    payload = (out or b"").decode("utf-8", errors="replace")
    err_text = (err or b"").decode("utf-8", errors="replace").strip()
    return decode_json(payload, err_text, proc.returncode), err_text


def download_command(job, outdir: str, prefix: list[str] | None = None) -> list[str]:
    """`-f` only when the user picked a format; otherwise the config wins.

    `--continue` is explicit (and wins over the user config) so a download
    paused by leaving the TUI resumes its `.part` file next time.
    """
    cmd = list(prefix or ytdlp_prefix())
    selector = job.selector()
    if selector:
        cmd += ["-f", selector]
    cmd += ["-P", f"home:{outdir}", "--continue", "--newline", job.url]
    return cmd


# --------------------------------------------------------------------------- #
# download pool
# --------------------------------------------------------------------------- #
async def _default_spawn(cmd: list[str], env: dict | None = None):
    return await asyncio.create_subprocess_exec(
        *cmd,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.STDOUT,
        env=env or child_env(),
    )


class DownloadManager:
    """Bounded pool of yt-dlp processes, one per queued job."""

    def __init__(
        self,
        *,
        outdir: str,
        max_concurrent: int = 1,
        on_update=None,
        spawn=None,
        prefix: list[str] | None = None,
    ) -> None:
        self.outdir = outdir
        self.prefix = prefix
        self.max_concurrent = max(1, int(max_concurrent))
        self.on_update = on_update
        self._spawn = spawn or _default_spawn
        self._queue: asyncio.Queue = asyncio.Queue()
        self._workers: list[asyncio.Task] = []
        self._running: dict[int, object] = {}
        self._queued: set[int] = set()
        self._max_observed = 0

    # ------------------------------------------------------------- lifecycle
    def start(self) -> None:
        if self._workers:
            return
        self._workers = [
            asyncio.create_task(self._worker()) for _ in range(self.max_concurrent)
        ]

    async def stop(self) -> None:
        for proc in list(self._running.values()):
            terminate(proc)
        for worker in self._workers:
            worker.cancel()
        self._workers = []

    # --------------------------------------------------------------- queueing
    def submit(self, jobs) -> int:
        added = 0
        for job in jobs:
            if job.uid in self._queued or job.uid in self._running:
                continue
            self._queued.add(job.uid)
            self._queue.put_nowait(job)
            added += 1
        return added

    def cancel(self, uid: int) -> bool:
        proc = self._running.get(uid)
        if proc is None:
            return False
        terminate(proc)
        return True

    def kill_all(self) -> None:
        """Terminate every live process without waiting (used on app exit)."""
        for proc in list(self._running.values()):
            terminate(proc)

    @property
    def busy(self) -> bool:
        return bool(self._running) or not self._queue.empty()

    @property
    def running_count(self) -> int:
        return len(self._running)

    @property
    def max_observed(self) -> int:
        return self._max_observed

    # ---------------------------------------------------------------- internals
    def _emit(self, job) -> None:
        if self.on_update is not None:
            self.on_update(job)

    async def _worker(self) -> None:
        while True:
            job = await self._queue.get()
            self._queued.discard(job.uid)
            try:
                await self._run(job)
            except asyncio.CancelledError:
                raise
            except Exception as exc:  # noqa: BLE001 - one row must not kill the pool
                job.status = Status.ERROR
                job.error = str(exc)[:120]
                self._emit(job)
            finally:
                self._queue.task_done()

    async def _run(self, job) -> None:
        job.status = Status.DOWNLOADING
        job.percent = 0.0
        job.error = ""
        job.speed = ""
        job.eta = ""
        job.size = ""
        self._emit(job)

        prefix = self.prefix or ytdlp_prefix()
        cmd = download_command(job, self.outdir, prefix=prefix)
        proc = await self._spawn(
            cmd, child_env(extra_path=module_bin_dirs(prefix))
        )
        self._running[job.uid] = proc
        self._max_observed = max(self._max_observed, len(self._running))
        last_error = ""
        try:
            stream = proc.stdout
            while True:
                raw = await stream.readline()
                if not raw:
                    break
                line = raw.decode("utf-8", errors="replace").rstrip()
                if "Destination:" in line:
                    job.path = line.split("Destination:", 1)[1].strip()
                if line.startswith("ERROR:"):
                    last_error = line[6:].strip()
                progress = parse_progress(line)
                if progress:
                    job.percent = progress["percent"]
                    job.speed = progress["speed"]
                    job.eta = progress["eta"] or ""
                    job.size = progress["size"]
                    self._emit(job)
            code = await proc.wait()
        finally:
            self._running.pop(job.uid, None)

        if job.status is Status.CANCELLED:
            self._emit(job)
            return
        if code == 0:
            job.status = Status.DONE
            job.percent = 100.0
        else:
            job.status = Status.ERROR
            job.error = (last_error or f"exit {code}")[:120]
        self._emit(job)


def terminate(proc) -> None:
    terminate_fn = getattr(proc, "terminate", None)
    if terminate_fn is not None:
        try:
            terminate_fn()
        except ProcessLookupError:
            pass
