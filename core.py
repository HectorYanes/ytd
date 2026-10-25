"""yt-dlp boundary: child environment, progress parsing, process runners."""

from __future__ import annotations

import asyncio
import json
import os
import re
import shutil
from pathlib import Path

from model import Status


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



# --------------------------------------------------------------------------- #
# analysis
# --------------------------------------------------------------------------- #
class YtdlpError(RuntimeError):
    """Raised when yt-dlp cannot produce usable JSON."""


def binary_path() -> str:
    return shutil.which("yt-dlp") or "yt-dlp"


def analyze_command(url: str, *, flat: bool = False) -> list[str]:
    cmd = [binary_path(), "-J", "--no-warnings"]
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
    cmd = analyze_command(url, flat=flat)
    try:
        proc = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            env=child_env(),
        )
    except FileNotFoundError as exc:
        raise YtdlpError("yt-dlp not found in PATH") from exc
    try:
        out, err = await asyncio.wait_for(proc.communicate(), timeout=timeout)
    except asyncio.TimeoutError as exc:
        proc.kill()
        raise YtdlpError(f"yt-dlp timed out after {timeout:.0f}s") from exc
    payload = (out or b"").decode(errors="replace")
    err_text = (err or b"").decode(errors="replace").strip()
    return decode_json(payload, err_text, proc.returncode), err_text


def download_command(job, outdir: str, binary: str | None = None) -> list[str]:
    """`-f` only when the user picked a format; otherwise the config wins."""
    cmd = [binary or binary_path()]
    selector = job.selector()
    if selector:
        cmd += ["-f", selector]
    cmd += ["-P", f"home:{outdir}", "--newline", job.url]
    return cmd


# --------------------------------------------------------------------------- #
# download pool
# --------------------------------------------------------------------------- #
async def _default_spawn(cmd: list[str]):
    return await asyncio.create_subprocess_exec(
        *cmd,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.STDOUT,
        env=child_env(),
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
    ) -> None:
        self.outdir = outdir
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

        cmd = download_command(job, self.outdir)
        proc = await self._spawn(cmd)
        self._running[job.uid] = proc
        self._max_observed = max(self._max_observed, len(self._running))
        last_error = ""
        try:
            stream = proc.stdout
            while True:
                raw = await stream.readline()
                if not raw:
                    break
                line = raw.decode(errors="replace").rstrip()
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
