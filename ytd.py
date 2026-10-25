#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = ["textual>=1.0"]
# ///
"""ytd — a format explorer TUI for yt-dlp.

Lists every format yt-dlp reports for a URL, shows what is actually
downloadable, lets you filter the table, and downloads the exact
selection (including the video+audio pair yt-dlp needs to merge).

Usage:  ytd [URL]
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import re
import shutil
import sys
from dataclasses import dataclass, field
from pathlib import Path

from textual import work
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, VerticalScroll
from textual.widgets import (
    Checkbox,
    DataTable,
    Footer,
    Header,
    Input,
    ProgressBar,
    RichLog,
    Select,
    Static,
)
from rich.text import Text

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


# --------------------------------------------------------------------------- #
# pure helpers
# --------------------------------------------------------------------------- #
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
# TUI
# --------------------------------------------------------------------------- #
CSS = """
Screen { layout: vertical; }
#urlbar { height: 3; padding: 0 1; }
#meta { height: auto; padding: 0 1; color: $text-muted; }
#report { height: auto; max-height: 8; padding: 0 1; border: round $panel; }
#filters { height: 3; padding: 0 1; }
#filters Input { width: 1fr; }
#filters Select { width: 18; }
#filters Checkbox { width: auto; padding: 0 1; }
#formats { height: 1fr; min-height: 3; }
#slots { height: 2; padding: 0 1; background: $panel; }
#progress { height: 1; }
#log { height: 6; border-top: solid $panel; display: none; }
#log.visible { display: block; }
"""


class YtdApp(App):
    TITLE = "ytd — yt-dlp format explorer"
    CSS = CSS
    BINDINGS = [
        Binding("v", "pick_video", "Video", show=True),
        Binding("a", "pick_audio", "Audio", show=True),
        Binding("d", "download", "Download", show=True),
        Binding("y", "copy_command", "Copy cmd", show=True),
        Binding("f", "focus_filter", "Filter", show=True),
        Binding("l", "toggle_log", "Log", show=True),
        Binding("r", "reset", "Reset", show=True),
        Binding("q", "quit", "Quit", show=True),
    ]

    def __init__(
        self, info: dict | None = None, outdir: str | None = None, start_url: str | None = None
    ) -> None:
        super().__init__()
        self.start_url = start_url
        self.info: dict | None = info
        self.fmts: list[Fmt] = parse_formats(info) if info else []
        self._visible: list[Fmt] = []
        self.selected_video: str | None = None
        self.selected_audio: str | None = None
        self.outdir = outdir or str(Path.home() / "Downloads")
        self.merge_format = detect_merge_format()
        self.log_lines: list[str] = []
        self.meta_text = "No video loaded."
        self.report_text = ""

    # ---------------------------------------------------------------- layout
    def compose(self) -> ComposeResult:
        yield Header()
        yield Input(
            placeholder="Paste a video URL and press Enter (yt-dlp supported sites)",
            id="urlbar",
        )
        yield Static("", id="meta")
        yield VerticalScroll(Static("", id="report"))
        with Horizontal(id="filters"):
            yield Input(placeholder="filter: 1080, av1, opus, muxed…", id="filter")
            yield Select(
                [
                    ("all kinds", "all"),
                    ("muxed", "muxed"),
                    ("video only", "video"),
                    ("audio only", "audio"),
                    ("images", "image"),
                ],
                value="all",
                allow_blank=False,
                id="kind",
            )
            yield Select(
                [("any ext", "all"), ("mp4", "mp4"), ("webm", "webm"), ("m4a", "m4a")],
                value="all",
                allow_blank=False,
                id="container",
            )
            yield Checkbox("only sized", False, id="only_size")
            yield Checkbox("HLS", True, id="hls")
        yield DataTable(id="formats", cursor_type="row", zebra_stripes=True)
        yield Static("", id="slots")
        yield ProgressBar(total=100, show_eta=False, id="progress")
        yield RichLog(id="log", markup=True, wrap=True)
        yield Footer()

    def on_mount(self) -> None:
        self.query_one("#meta", Static).update(self.meta_text)
        table = self.table()
        table.add_columns(
            "sel", "id", "kind", "res", "vcodec", "acodec", "hdr", "fps",
            "br", "size", "ext", "src", "note",
        )
        self.query_one("#progress", ProgressBar).update(total=100, progress=0)
        if self.info:
            self.load_info(self.info)
        elif self.start_url:
            self.query_one("#urlbar", Input).value = self.start_url
            self.analyze(self.start_url)

    # ------------------------------------------------------------- utilities
    def table(self) -> DataTable:
        return self.query_one("#formats", DataTable)

    def row_count(self) -> int:
        return self.table().row_count

    def visible_ids(self) -> list[str]:
        return [f.id for f in self._visible]

    def selector(self) -> str:
        return build_format_selector(self.selected_video, self.selected_audio)

    def show_log(self) -> None:
        self.query_one("#log", RichLog).add_class("visible")

    def action_toggle_log(self) -> None:
        self.query_one("#log", RichLog).toggle_class("visible")

    def write_log(self, message: str) -> None:
        plain = re.sub(r"\[/?[a-z #0-9]+\]", "", message)
        self.log_lines.append(plain)
        try:
            self.query_one("#log", RichLog).write(message)
        except Exception:
            pass

    # ------------------------------------------------------------- filtering
    def current_filters(self) -> dict:
        return {
            "text": self.query_one("#filter", Input).value,
            "kind": self.query_one("#kind", Select).value or "all",
            "container": self.query_one("#container", Select).value or "all",
            "only_with_size": self.query_one("#only_size", Checkbox).value,
            "include_hls": self.query_one("#hls", Checkbox).value,
        }

    def refresh_rows(self) -> None:
        self._visible = apply_filters(self.fmts, **self.current_filters())
        table = self.table()
        cursor = table.cursor_row
        table.clear()
        for fmt in self._visible:
            table.add_row(*self.cells_for(fmt), key=fmt.id)
        if self._visible:
            table.move_cursor(row=min(cursor, len(self._visible) - 1))

    def cells_for(self, fmt: Fmt) -> list:
        style = KIND_STYLE.get(fmt.kind, "")
        mark = ""
        if fmt.id == self.selected_video:
            mark = "V"
        if fmt.id == self.selected_audio:
            mark = "A" if not mark else "V+A"
        note = fmt.note or ""
        if fmt.language:
            note = f"{note} [{fmt.language}]".strip()
        return [
            Text(mark, style="bold yellow"),
            Text(fmt.id, style=f"bold {style}"),
            Text(fmt.kind, style=style),
            Text(fmt.resolution),
            Text(fmt.vcodec_name or "—"),
            Text(fmt.acodec_name or "—"),
            Text(fmt.hdr_label or "—", style="magenta" if fmt.hdr_label else ""),
            Text(f"{int(fmt.fps)}" if fmt.fps and fmt.fps > 1 else "—"),
            Text(human_bitrate(fmt.tbr)),
            Text(human_size(fmt.size)),
            Text(fmt.ext),
            Text("hls" if fmt.is_hls else (fmt.protocol or "—")),
            Text(note),
        ]

    def on_input_changed(self, event: Input.Changed) -> None:
        if event.input.id == "filter":
            self.refresh_rows()

    def on_select_changed(self, event: Select.Changed) -> None:
        self.refresh_rows()

    def on_checkbox_changed(self, event: Checkbox.Changed) -> None:
        self.refresh_rows()

    # ---------------------------------------------------------------- loading
    def load_info(self, info: dict) -> None:
        self.info = info
        self.fmts = parse_formats(info)
        title = info.get("title") or info.get("id") or "untitled"
        bits = [f"[bold]{title}[/bold]"]
        if info.get("uploader"):
            bits.append(info["uploader"])
        if info.get("duration"):
            bits.append(human_duration(info["duration"]))
        if info.get("is_live"):
            bits.append("[red]LIVE[/red]")
        bits.append(f"{len(self.fmts)} formats")
        bits.append(f"saves to {self.outdir}")
        self.meta_text = "  ·  ".join(bits)
        self.query_one("#meta", Static).update(self.meta_text)

        lines = availability_report(self.fmts, self.merge_format)
        self.report_text = "\n".join(lines) if lines else "No formats."
        self.query_one("#report", Static).update(self.report_text)
        self.refresh_rows()
        self.write_log(f"Loaded {len(self.fmts)} formats for {title}")

    def on_input_submitted(self, event: Input.Submitted) -> None:
        if event.input.id == "urlbar":
            url = event.value.strip()
            if url:
                self.analyze(url)

    @work(exclusive=True, group="analyze")
    async def analyze(self, url: str) -> None:
        binary = shutil.which("yt-dlp") or "yt-dlp"
        cmd = [binary, "-J", "--no-warnings", "--no-playlist", url]
        self.write_log(f"[dim]$ {' '.join(cmd)}[/dim]")
        try:
            proc = await asyncio.create_subprocess_exec(
                *cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                env=child_env(),
            )
            out, err = await asyncio.wait_for(proc.communicate(), timeout=180)
        except FileNotFoundError:
            self.show_log()
            self.write_log("[red]yt-dlp not found in PATH[/red]")
            return
        except asyncio.TimeoutError:
            self.write_log("[red]yt-dlp timed out after 180s[/red]")
            return
        err_text = (err or b"").decode(errors="replace").strip()
        payload = (out or b"").decode(errors="replace")
        if proc.returncode != 0 and not payload.strip():
            self.show_log()
            tail = err_text.splitlines()[-3:]
            self.write_log("[red]yt-dlp failed:[/red] " + " ".join(tail))
            return
        try:
            info = json.loads(payload)
        except json.JSONDecodeError:
            tail = err_text.splitlines()[-3:]
            self.write_log("[red]Could not parse yt-dlp JSON. " + " ".join(tail) + "[/red]")
            return
        if err_text:
            self.write_log(f"[yellow]yt-dlp warned:[/yellow] {err_text.splitlines()[-1][:160]}")
        if info.get("_type") == "playlist":
            entries = info.get("entries") or []
            self.write_log(
                f"[yellow]Playlist with {len(entries)} entries. "
                f"Paste the URL of a single video.[/yellow]"
            )
            return
        self.load_info(info)

    # -------------------------------------------------------------- selection
    def _find(self, fmt_id: str) -> Fmt | None:
        return next((f for f in self.fmts if f.id == fmt_id), None)

    def select_video(self, fmt_id: str) -> bool:
        fmt = self._find(fmt_id)
        if not fmt or fmt.kind not in ("video", "muxed"):
            return False
        self.selected_video = fmt.id
        if fmt.kind == "muxed":
            self.selected_audio = None
        self.refresh_rows()
        self.update_slots()
        return True

    def select_audio(self, fmt_id: str) -> bool:
        fmt = self._find(fmt_id)
        if not fmt or fmt.kind != "audio":
            return False
        self.selected_audio = fmt.id
        self.refresh_rows()
        self.update_slots()
        return True

    def current_row_id(self) -> str | None:
        if not self._visible:
            return None
        index = self.table().cursor_row
        if 0 <= index < len(self._visible):
            return self._visible[index].id
        return None

    def update_slots(self) -> None:
        def describe(fmt_id: str | None) -> str:
            if not fmt_id:
                return "[dim]—[/dim]"
            fmt = self._find(fmt_id)
            if not fmt:
                return fmt_id
            codec = fmt.vcodec_name or fmt.acodec_name or "?"
            return f"{fmt.id} ({codec}, {fmt.resolution}, {human_size(fmt.size)})"

        selector = self.selector()
        warn = ""
        if self.selected_video and not self.selected_audio:
            fmt = self._find(self.selected_video)
            if fmt and fmt.kind == "video":
                warn = "  [yellow]⚠ no audio selected: the file will be silent[/yellow]"
        self.query_one("#slots", Static).update(
            f"video: {describe(self.selected_video)}    "
            f"audio: {describe(self.selected_audio)}    "
            f"[bold]-f {selector or '…'}[/bold]{warn}"
        )

    def action_pick_video(self) -> None:
        fmt_id = self.current_row_id()
        if not fmt_id:
            return
        if not self.select_video(fmt_id):
            self.notify("That row is not a video format.", severity="warning")

    def action_pick_audio(self) -> None:
        fmt_id = self.current_row_id()
        if not fmt_id:
            return
        if not self.select_audio(fmt_id):
            self.notify("That row is not an audio-only format.", severity="warning")

    def action_reset(self) -> None:
        self.selected_video = None
        self.selected_audio = None
        self.refresh_rows()
        self.update_slots()

    def action_focus_filter(self) -> None:
        self.query_one("#filter", Input).focus()

    # -------------------------------------------------------------- download
    def download_command(self) -> list[str]:
        binary = shutil.which("yt-dlp") or "yt-dlp"
        url = (self.info or {}).get("webpage_url") or self.query_one("#urlbar", Input).value
        cmd = [binary, "-f", self.selector(), "-P", f"home:{self.outdir}", "--newline"]
        if url:
            cmd.append(url)
        return cmd

    def action_copy_command(self) -> None:
        if not self.selector():
            self.notify("Pick a video and/or audio row first.", severity="warning")
            return
        cmd = " ".join(self.download_command())
        self.copy_to_clipboard(cmd)
        self.notify("Command copied to clipboard.")

    def action_download(self) -> None:
        if not self.selector():
            self.notify("Pick a video (v) and/or audio (a) row first.", severity="warning")
            return
        video = self._find(self.selected_video) if self.selected_video else None
        if video and video.kind == "video" and not self.selected_audio:
            self.notify("Video-only selection: no audio track included.", severity="warning")
        self.run_download(self.download_command())

    @work(exclusive=True, group="download")
    async def run_download(self, cmd: list[str]) -> None:
        self.show_log()
        self.write_log(f"[dim]$ {' '.join(cmd)}[/dim]")
        bar = self.query_one("#progress", ProgressBar)
        bar.update(total=100, progress=0)
        try:
            proc = await asyncio.create_subprocess_exec(
                *cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.STDOUT,
                env=child_env(),
            )
        except FileNotFoundError:
            self.write_log("[red]yt-dlp not found in PATH[/red]")
            return
        assert proc.stdout is not None
        while True:
            raw = await proc.stdout.readline()
            if not raw:
                break
            line = raw.decode(errors="replace").rstrip()
            if not line:
                continue
            progress = parse_progress(line)
            if progress:
                bar.update(progress=progress["percent"])
                self.write_log(
                    f"[cyan]{progress['percent']:5.1f}%[/cyan] of {progress['size']} "
                    f"at {progress['speed']}"
                    + (f" ETA {progress['eta']}" if progress["eta"] else "")
                )
            elif line.startswith("["):
                self.write_log(f"[bright_black]{line}[/bright_black]")
            else:
                self.write_log(line)
        code = await proc.wait()
        if code == 0:
            bar.update(progress=100)
            self.write_log(f"[green]Done → {self.outdir}[/green]")
        else:
            self.write_log(f"[red]yt-dlp exited with {code}[/red]")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="ytd", description="Format explorer TUI for yt-dlp."
    )
    parser.add_argument("url", nargs="?", help="video URL to analyze on start")
    parser.add_argument(
        "-o", "--outdir", default=None, help="download directory (default: ~/Downloads)"
    )
    args = parser.parse_args(argv)
    app = YtdApp(outdir=args.outdir, start_url=args.url)
    app.run()
    return 0


if __name__ == "__main__":
    sys.exit(main())
