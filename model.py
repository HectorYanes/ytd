"""Data model: formats, filtering, availability and compatibility rules."""

from __future__ import annotations

from dataclasses import dataclass, field


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

