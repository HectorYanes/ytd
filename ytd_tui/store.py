"""Queue persistence: the TUI dies, the queue does not.

State lives outside the project and outside the download folder, one JSON file
per user. Reads are defensive on purpose: a corrupt or foreign file must never
block the app from starting, it just starts with an empty queue.
"""

from __future__ import annotations

import json
import os
import platform
import tempfile
from dataclasses import dataclass, field, fields
from pathlib import Path

from .model import Fmt, Job, Status

QUEUE_VERSION = 1


# --------------------------------------------------------------------------- #
# location
# --------------------------------------------------------------------------- #
def state_dir(
    system: str | None = None,
    home: Path | str | None = None,
    environ: dict | None = None,
) -> Path:
    """Per-user state directory, honoring XDG on every POSIX system."""
    system = system or platform.system()
    home = Path(home) if home is not None else Path.home()
    environ = dict(os.environ) if environ is None else environ

    if system == "Windows":
        base = environ.get("LOCALAPPDATA") or str(home / "AppData" / "Local")
        return Path(base) / "ytd"

    xdg = environ.get("XDG_STATE_HOME")
    if xdg:
        return Path(xdg) / "ytd"
    if system == "Darwin":
        return home / "Library" / "Application Support" / "ytd"
    return home / ".local" / "state" / "ytd"


def queue_path(**kwargs) -> Path:
    return state_dir(**kwargs) / "queue.json"


# --------------------------------------------------------------------------- #
# (de)serialization
# --------------------------------------------------------------------------- #
_FMT_FIELDS = {f.name for f in fields(Fmt)}
_JOB_KEYS = (
    "uid",
    "url",
    "title",
    "uploader",
    "duration",
    "video_id",
    "audio_id",
    "status",
    "percent",
    "speed",
    "eta",
    "size",
    "error",
    "path",
)


def _num(value, cast, default):
    try:
        return cast(value)
    except (TypeError, ValueError):
        return default


def _opt_num(value, cast):
    if value is None:
        return None
    try:
        return cast(value)
    except (TypeError, ValueError):
        return None


def _text(value) -> str:
    return value if isinstance(value, str) else ""


def _fmt_to_dict(fmt: Fmt) -> dict:
    data = {name: getattr(fmt, name) for name in _FMT_FIELDS}
    if not isinstance(data.get("extra"), dict):
        data["extra"] = {}
    return data


def _fmt_from_dict(entry) -> Fmt | None:
    if not isinstance(entry, dict):
        return None
    fmt_id = entry.get("id")
    if not isinstance(fmt_id, str) or not fmt_id:
        return None
    kwargs = {key: entry[key] for key in _FMT_FIELDS if key in entry and key != "id"}
    extra = kwargs.get("extra")
    if not isinstance(extra, dict):
        kwargs["extra"] = {}
    try:
        return Fmt(id=fmt_id, **kwargs)
    except TypeError:
        return None


def _job_to_dict(job: Job) -> dict:
    data = {
        "uid": job.uid,
        "status": job.status.value,
        "formats": [_fmt_to_dict(fmt) for fmt in job.formats],
    }
    for key in _JOB_KEYS:
        if key in data:
            continue
        data[key] = getattr(job, key)
    return data


def _job_from_dict(entry) -> Job | None:
    if not isinstance(entry, dict):
        return None
    url = entry.get("url")
    if not isinstance(url, str) or not url.strip():
        return None

    try:
        status = Status(entry.get("status"))
    except ValueError:
        status = Status.LOADED

    formats = [_fmt_from_dict(item) for item in entry.get("formats") or []]
    return Job(
        uid=_num(entry.get("uid"), int, 0),
        url=url.strip(),
        title=_text(entry.get("title")),
        uploader=_text(entry.get("uploader")),
        duration=_opt_num(entry.get("duration"), float),
        formats=[fmt for fmt in formats if fmt is not None],
        video_id=_text(entry.get("video_id")) or None,
        audio_id=_text(entry.get("audio_id")) or None,
        status=status,
        percent=_num(entry.get("percent"), float, 0.0),
        speed=_text(entry.get("speed")),
        eta=_text(entry.get("eta")),
        size=_text(entry.get("size")),
        error=_text(entry.get("error")),
        path=_text(entry.get("path")) or None,
    )


# --------------------------------------------------------------------------- #
# store
# --------------------------------------------------------------------------- #
@dataclass
class Restored:
    """What came back from disk, plus which rows must be re-queued."""

    jobs: list[Job] = field(default_factory=list)
    resume: list[int] = field(default_factory=list)
    warning: str = ""


class QueueStore:
    def __init__(self, path: Path | str | None = None) -> None:
        self.path = Path(path) if path is not None else queue_path()

    def load(self) -> Restored:
        try:
            raw = self.path.read_text(encoding="utf-8")
        except FileNotFoundError:
            return Restored()
        except OSError as exc:
            return Restored(warning=f"queue: cannot read {self.path}: {exc}")

        try:
            data = json.loads(raw)
        except (json.JSONDecodeError, UnicodeDecodeError):
            return Restored(warning=f"queue: ignoring unreadable {self.path.name}")
        if not isinstance(data, dict):
            return Restored(warning=f"queue: ignoring unexpected {self.path.name}")

        entries = data.get("jobs")
        if not isinstance(entries, list):
            return Restored()

        jobs: list[Job] = []
        resume: list[int] = []
        seen_urls: set[str] = set()
        seen_uids: set[int] = set()
        next_uid = 1
        for entry in entries:
            job = _job_from_dict(entry)
            if job is None or job.url in seen_urls:
                continue
            if job.uid <= 0 or job.uid in seen_uids:
                job.uid = next_uid
            seen_urls.add(job.url)
            seen_uids.add(job.uid)
            next_uid = max(next_uid, job.uid + 1)

            if job.status is Status.DOWNLOADING:
                # A paused download comes back as queued and gets re-submitted.
                job.status = Status.LOADED
                resume.append(job.uid)
            jobs.append(job)

        return Restored(jobs=jobs, resume=resume)

    def save(self, jobs) -> None:
        payload = {
            "version": QUEUE_VERSION,
            "jobs": [_job_to_dict(job) for job in jobs],
        }
        self.path.parent.mkdir(parents=True, exist_ok=True)
        handle = tempfile.NamedTemporaryFile(
            "w",
            encoding="utf-8",
            dir=self.path.parent,
            prefix=".queue-",
            suffix=".tmp",
            delete=False,
        )
        try:
            with handle:
                json.dump(payload, handle, ensure_ascii=False, indent=1)
                handle.write("\n")
            os.replace(handle.name, self.path)
        except BaseException:
            try:
                os.unlink(handle.name)
            except OSError:
                pass
            raise
