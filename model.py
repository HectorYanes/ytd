"""Data model: formats, filtering, availability and compatibility rules."""

from __future__ import annotations

from dataclasses import dataclass, field
from enum import Enum


YOUTUBE_HLS = "m3u8"

CODEC_ALIASES = (
    ("avc1", "h264"),
    ("avc3", "h264"),
    ("hev1", "h265"),
    ("hvc1", "h265"),
    ("av01", "av1"),
    ("vp09", "vp9"),
    ("vp9", "vp9"),
    ("vp08", "vp8"),
    ("mp4a", "aac"),
    ("opus", "opus"),
    ("vorbis", "vorbis"),
    ("ec-3", "eac3"),
    ("ac-3", "ac3"),
    ("dts", "dts"),
    ("flac", "flac"),
)

KIND_STYLE = {
    "muxed": "yellow",
    "video": "cyan",
    "audio": "green",
    "image": "bright_black",
}


def codec_name(raw: str | None) -> str | None:
    """Map a yt-dlp codec string to a short human name."""
    if not raw or raw == "none":
        return None
    low = raw.lower()
    for needle, name in CODEC_ALIASES:
        if low.startswith(needle) or needle in low:
            return name
    return low.split(".")[0]


def human_size(size: int | None) -> str:
    if size is None:
        return "—"
    if size < 1024:
        return f"{size} B"
    value = float(size)
    for unit in ("KiB", "MiB", "GiB", "TiB"):
        value /= 1024
        if value < 1024:
            return f"{value:.1f} {unit}"
    return f"{value:.1f} PiB"


def human_duration(seconds: float | int | None) -> str:
    if not seconds:
        return "—"
    total = int(seconds)
    hours, rest = divmod(total, 3600)
    minutes, secs = divmod(rest, 60)
    if hours:
        return f"{hours}:{minutes:02d}:{secs:02d}"
    return f"{minutes}:{secs:02d}"


def human_bitrate(kbps: float | None) -> str:
    if not kbps:
        return "—"
    return f"{kbps / 1000:.1f}M" if kbps >= 1000 else f"{kbps:.0f}k"


@dataclass
class Fmt:
    id: str
    ext: str
    vcodec: str | None
    acodec: str | None
    height: int | None = None
    width: int | None = None
    fps: float | None = None
    tbr: float | None = None
    abr: float | None = None
    size: int | None = None
    note: str | None = None
    hdr: str | None = None
    language: str | None = None
    protocol: str | None = None
    kind: str = "video"
    available: bool = True
    extra: dict = field(default_factory=dict)

    @property
    def vcodec_name(self) -> str | None:
        return codec_name(self.vcodec)

    @property
    def acodec_name(self) -> str | None:
        return codec_name(self.acodec)

    @property
    def is_hls(self) -> bool:
        return YOUTUBE_HLS in (self.protocol or "")

    @property
    def resolution(self) -> str:
        if self.kind == "audio":
            return "audio"
        if self.kind == "image":
            return f"{self.height}p" if self.height else "image"
        if not self.height:
            return "—"
        suffix = f"{int(self.fps)}" if self.fps and self.fps > 30 else ""
        return f"{self.height}p{suffix}"

    @property
    def hdr_label(self) -> str:
        if not self.hdr or self.hdr == "SDR":
            return ""
        return self.hdr

    @property
    def haystack(self) -> str:
        parts = [
            self.id,
            self.ext,
            self.kind,
            self.resolution,
            self.vcodec_name or "",
            self.acodec_name or "",
            self.hdr_label,
            self.note or "",
            self.language or "",
            str(self.height or ""),
        ]
        return " ".join(parts).lower()


def _classify(fmt: dict) -> tuple[str, bool]:
    vcodec = fmt.get("vcodec")
    acodec = fmt.get("acodec")
    has_v = bool(vcodec and vcodec != "none")
    has_a = bool(acodec and acodec != "none")
    if fmt.get("ext") in ("mhtml", "mhtml_list") or (not has_v and not has_a):
        return "image", False
    if has_v and has_a:
        return "muxed", True
    if has_v:
        return "video", True
    return "audio", True


def parse_formats(info: dict) -> list[Fmt]:
    """Turn a yt-dlp info dict into Fmt rows, preserving yt-dlp's order."""
    out: list[Fmt] = []
    for raw in info.get("formats") or []:
        if not raw.get("format_id"):
            continue
        kind, available = _classify(raw)
        size = raw.get("filesize") or raw.get("filesize_approx")
        out.append(
            Fmt(
                id=str(raw["format_id"]),
                ext=raw.get("ext") or "?",
                vcodec=raw.get("vcodec"),
                acodec=raw.get("acodec"),
                height=raw.get("height"),
                width=raw.get("width"),
                fps=raw.get("fps"),
                tbr=raw.get("tbr"),
                abr=raw.get("abr"),
                size=size,
                note=raw.get("format_note"),
                hdr=raw.get("dynamic_range"),
                language=raw.get("language"),
                protocol=raw.get("protocol"),
                kind=kind,
                available=available,
            )
        )
    return out


def apply_filters(
    fmts: list[Fmt],
    *,
    text: str = "",
    kind: str = "all",
    container: str = "all",
    only_with_size: bool = False,
    include_hls: bool = True,
    show_images: bool = False,
) -> list[Fmt]:
    needle = text.strip().lower()
    out: list[Fmt] = []
    for fmt in fmts:
        if not fmt.available and not show_images:
            continue
        if kind == "image" and fmt.kind != "image":
            continue
        if kind != "all" and kind != "image" and fmt.kind != kind:
            continue
        if container != "all" and fmt.ext != container:
            continue
        if only_with_size and not fmt.size:
            continue
        if not include_hls and fmt.is_hls:
            continue
        if needle and needle not in fmt.haystack:
            continue
        out.append(fmt)
    return out


def availability_report(fmts: list[Fmt], merge_format: str | None = None) -> list[str]:
    """Human summary of what is actually downloadable, grouped by height."""
    lines: list[str] = []
    videos = [f for f in fmts if f.available and f.height and f.kind in ("video", "muxed")]
    audios = [f for f in fmts if f.kind == "audio"]

    heights = sorted({f.height for f in videos}, reverse=True)
    for height in heights:
        group = [f for f in videos if f.height == height]
        fps = max((f.fps or 0) for f in group)
        codecs = sorted({f.vcodec_name for f in group if f.vcodec_name})
        muxed = [f for f in group if f.kind == "muxed"]
        progressive = [f for f in muxed if not f.is_hls]
        hls_only = bool(muxed) and not progressive

        bits = [f"[bold]{height}p{int(fps) if fps > 30 else ''}[/bold]"]
        bits.append(", ".join(codecs) if codecs else "—")
        hdrs = sorted({f.hdr_label for f in group if f.hdr_label})
        if hdrs:
            bits.append(f"[magenta]{'/'.join(hdrs)}[/magenta]")
        if progressive:
            bits.append("[green]muxed ready[/green]")
        elif hls_only:
            bits.append("[yellow]muxed via HLS[/yellow]")
        else:
            bits.append("[cyan]video-only[/cyan]")
        lines.append("  · ".join(bits))

    if audios:
        items = []
        for codec in sorted({a.acodec_name for a in audios if a.acodec_name}):
            best = max(
                (a for a in audios if a.acodec_name == codec),
                key=lambda a: a.abr or a.tbr or 0,
            )
            items.append(f"{codec} {human_bitrate(best.abr or best.tbr)}")
        lines.append("  [green]audio[/green] (best per codec)  ·  " + ", ".join(items))

    warnings: list[str] = []
    if heights:
        top = heights[0]
        top_group = [f for f in videos if f.height == top]
        if not any(f.kind == "muxed" and not f.is_hls for f in top_group):
            warnings.append(
                f"⚠ {top}p is video-only: it needs a separate audio stream "
                f"(yt-dlp merges them with ffmpeg)."
            )
    if merge_format == "mp4" and any(a.acodec_name == "opus" for a in audios):
        warnings.append(
            "⚠ best audio is opus and your merge container is mp4: "
            "QuickTime and older players may not read it."
        )
    missing = [f.id for f in fmts if f.available and not f.size]
    if missing:
        shown = ", ".join(missing[:4]) + ("…" if len(missing) > 4 else "")
        warnings.append(f"· {len(missing)} format(s) report no size ({shown}): size is known after the first bytes.")

    return lines + warnings


def build_format_selector(video_id: str | None, audio_id: str | None) -> str:
    if video_id and audio_id:
        return f"{video_id}+{audio_id}"
    return video_id or audio_id or ""



# --------------------------------------------------------------------------- #
# queue model
# --------------------------------------------------------------------------- #
class Status(str, Enum):
    """Job states. Labels follow the vocabulary the user asked for."""

    LOADED = "cargada"
    DOWNLOADING = "descargando"
    DONE = "completado"
    ERROR = "error"
    CANCELLED = "cancelado"


STATUS_STYLE = {
    Status.LOADED: "white",
    Status.DOWNLOADING: "cyan",
    Status.DONE: "green",
    Status.ERROR: "red",
    Status.CANCELLED: "yellow",
}

def status_label(status: Status, percent: float = 0.0, error: str = "") -> str:
    if status is Status.DOWNLOADING:
        return f"descargando ({percent:.0f}%)"
    if status is Status.ERROR:
        return f"error: {error}" if error else "error"
    return status.value


def default_selection(
    formats: list[Fmt], prefer_audio_codec: str | None = None
) -> tuple[str | None, str | None]:
    """Best video (+ audio when the video stream carries none)."""
    videos = [f for f in formats if f.available and f.kind == "video" and not f.is_hls]
    if not videos:
        videos = [f for f in formats if f.available and f.kind == "video"]
    muxed = [f for f in formats if f.available and f.kind == "muxed" and not f.is_hls]
    if not muxed:
        muxed = [f for f in formats if f.available and f.kind == "muxed"]

    video: Fmt | None = None
    if videos:
        video = max(videos, key=lambda f: (f.height or 0, f.fps or 0, f.tbr or 0))
    elif muxed:
        video = max(muxed, key=lambda f: (f.height or 0, f.fps or 0, f.tbr or 0))
    if video is None:
        return None, None
    if video.kind == "muxed":
        return video.id, None

    audios = [f for f in formats if f.kind == "audio"]
    if not audios:
        return video.id, None
    if prefer_audio_codec:
        preferred = [a for a in audios if a.acodec_name == prefer_audio_codec]
        if preferred:
            audios = preferred
    audio = max(audios, key=lambda f: (f.abr or f.tbr or 0, f.size or 0))
    return video.id, audio.id


@dataclass
class Job:
    """One queue row: a URL plus its formats, choice and download state."""

    uid: int
    url: str
    title: str = ""
    uploader: str = ""
    duration: float | None = None
    formats: list[Fmt] = field(default_factory=list)
    video_id: str | None = None
    audio_id: str | None = None
    status: Status = Status.LOADED
    percent: float = 0.0
    speed: str = ""
    eta: str = ""
    size: str = ""
    error: str = ""
    path: str | None = None

    @property
    def analyzed(self) -> bool:
        return bool(self.formats)

    def find(self, fmt_id: str | None) -> Fmt | None:
        if not fmt_id:
            return None
        return next((f for f in self.formats if f.id == fmt_id), None)

    def video(self) -> Fmt | None:
        return self.find(self.video_id)

    def audio(self) -> Fmt | None:
        return self.find(self.audio_id)

    def selector(self) -> str:
        """Explicit `-f` value, empty when yt-dlp should use the config default."""
        return build_format_selector(self.video_id, self.audio_id)

    def label(self, limit: int = 58) -> str:
        text = self.title or self.url
        return text if len(text) <= limit else text[: limit - 1] + "…"

    def quality_label(self) -> str:
        if not self.analyzed or not self.video_id:
            return "auto (best)"
        fmt = self.video()
        if fmt is None:
            return "auto (best)"
        codec = fmt.vcodec_name or "?"
        if fmt.kind == "muxed":
            return f"{fmt.resolution} · {codec} (muxed)"
        return f"{fmt.resolution} · {codec}"

    def audio_label(self) -> str:
        video = self.video()
        if self.analyzed and video is not None and video.kind == "muxed":
            return "— (incluido)"
        if not self.analyzed or not self.audio_id:
            return "auto (best)"
        fmt = self.audio()
        if fmt is None:
            return "auto (best)"
        return f"{fmt.acodec_name or '?'} {human_bitrate(fmt.abr or fmt.tbr)}"

    def expected_size(self) -> int | None:
        total = 0
        counted = False
        for fmt in (self.video(), self.audio()):
            if fmt is not None and fmt.size:
                total += fmt.size
                counted = True
        if counted:
            return total
        return self.video().size if self.video() and self.video().size else None

    def size_label(self) -> str:
        return human_size(self.expected_size())
