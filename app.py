"""Queue UI: one row per URL, with quality, audio and live download status."""

from __future__ import annotations

import re

from rich.text import Text
from textual import work
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, VerticalScroll
from textual.css.query import NoMatches
from textual.message import Message
from textual.widgets import (
    Checkbox,
    DataTable,
    Footer,
    Header,
    Input,
    RichLog,
    Select,
    Static,
)

from core import DownloadManager, YtdlpError, detect_merge_format, run_json
from model import (
    KIND_STYLE,
    STATUS_STYLE,
    Job,
    Status,
    apply_filters,
    availability_report,
    default_selection,
    status_label,
)

QUEUE_COLUMNS = (
    ("#", "num"),
    ("título", "title"),
    ("calidad", "quality"),
    ("audio", "audio"),
    ("estado", "status"),
    ("tamaño", "size"),
)

FORMAT_COLUMNS = (
    "sel", "id", "kind", "res", "vcodec", "acodec", "hdr", "fps",
    "br", "size", "ext", "src", "note",
)


class JobChanged(Message):
    """A job's fields changed; redraw only that row."""

    def __init__(self, uid: int) -> None:
        self.uid = uid
        super().__init__()


class YtdApp(App):
    TITLE = "ytd — yt-dlp queue"
    CSS = """
Screen { layout: vertical; }
#urlbar { height: 3; padding: 0 1; }
#queue { height: 1fr; min-height: 4; }
#report { height: auto; max-height: 6; padding: 0 1; border: round $panel; }
#filters { height: 3; padding: 0 1; }
#filters Input { width: 1fr; }
#filters Select { width: 17; }
#filters Checkbox { width: auto; padding: 0 1; }
#formats { height: 1fr; min-height: 3; }
#slots { height: 2; padding: 0 1; background: $panel; }
#log { height: 6; border-top: solid $panel; display: none; }
#log.visible { display: block; }
"""
    BINDINGS = [
        Binding("v", "pick_video", "Video", show=True),
        Binding("a", "pick_audio", "Audio", show=True),
        Binding("e", "edit_job", "Analyze", show=True),
        Binding("d", "download", "Download", show=True),
        Binding("s", "start_all", "Start all", show=True),
        Binding("x", "cancel_job", "Cancel", show=True),
        Binding("delete", "delete_job", "Remove", show=True),
        Binding("r", "reset_choice", "Reset", show=True),
        Binding("f", "focus_filter", "Filter", show=True),
        Binding("l", "toggle_log", "Log", show=True),
        Binding("q", "quit", "Quit", show=True),
    ]

    def __init__(
        self,
        *,
        outdir: str | None = None,
        start_urls: list[str] | None = None,
        max_concurrent: int = 1,
        spawn=None,
        seed: list[tuple[str, dict]] | None = None,
    ) -> None:
        super().__init__()
        from pathlib import Path

        self.outdir = outdir or str(Path.home() / "Downloads")
        self.start_urls = list(start_urls or [])
        self.max_concurrent = max_concurrent
        self._spawn = spawn
        self._seed = list(seed or [])
        self.jobs: list[Job] = []
        self.focused_uid: int | None = None
        self.manager: DownloadManager | None = None
        self.merge_format = detect_merge_format()
        self.log_lines: list[str] = []
        self.report_text = ""
        self._next_uid = 1
        self._visible: list = []

    # ---------------------------------------------------------------- layout
    def compose(self) -> ComposeResult:
        yield Header()
        yield Input(
            placeholder="Paste a URL (or several, space separated) and press Enter",
            id="urlbar",
        )
        yield DataTable(id="queue", cursor_type="row", zebra_stripes=True)
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
        yield RichLog(id="log", markup=True, wrap=True)
        yield Footer()

    def on_mount(self) -> None:
        queue = self.queue()
        for label, key in QUEUE_COLUMNS:
            queue.add_column(label, key=key)
        formats = self.formats_table()
        for name in FORMAT_COLUMNS:
            formats.add_column(name, key=name)
        self.manager = DownloadManager(
            outdir=self.outdir,
            max_concurrent=self.max_concurrent,
            on_update=lambda job: self.post_message(JobChanged(job.uid)),
            spawn=self._spawn,
        )
        self.manager.start()
        for url, info in self._seed:
            self.add_job_from_info(url, info)
        for url in self.start_urls:
            self.add_url(url)
        self.refresh_focused()
        self.query_one("#urlbar", Input).focus()

    # ------------------------------------------------------------- utilities
    def queue(self) -> DataTable:
        return self.query_one("#queue", DataTable)

    def formats_table(self) -> DataTable:
        return self.query_one("#formats", DataTable)

    def focused_job(self) -> Job | None:
        return self.job_by_uid(self.focused_uid)

    def job_by_uid(self, uid: int | None) -> Job | None:
        return next((j for j in self.jobs if j.uid == uid), None)

    def write_log(self, message: str) -> None:
        self.log_lines.append(re.sub(r"\[/?[a-z #0-9]+\]", "", message))
        try:
            self.query_one("#log", RichLog).write(message)
        except Exception:
            pass

    def show_log(self) -> None:
        try:
            self.query_one("#log", RichLog).add_class("visible")
        except Exception:
            pass

    # ------------------------------------------------------------ queue render
    def row_cells(self, job: Job) -> list:
        return [
            Text(str(self.jobs.index(job) + 1)),
            Text(job.label()),
            Text(job.quality_label(), style="cyan"),
            Text(job.audio_label(), style="green"),
            Text(
                status_label(job.status, job.percent, job.error),
                style=STATUS_STYLE.get(job.status, ""),
            ),
            Text(job.size_label()),
        ]

    def refresh_queue(self) -> None:
        table = self.queue()
        table.clear()
        for job in self.jobs:
            table.add_row(*self.row_cells(job), key=str(job.uid))
        if self.focused_uid is not None:
            for index, job in enumerate(self.jobs):
                if job.uid == self.focused_uid:
                    table.move_cursor(row=index)
                    break

    def update_row(self, uid: int) -> None:
        job = self.job_by_uid(uid)
        table = self.queue()
        if job is None or not table.row_count:
            return
        if not any(row_key.value == str(uid) for row_key in table.rows):
            self.refresh_queue()
            return
        for (_, key), cell in zip(QUEUE_COLUMNS, self.row_cells(job)):
            table.update_cell(str(uid), key, cell)

    # ---------------------------------------------------------- job lifecycle
    def add_url(self, url: str) -> None:
        url = url.strip()
        if not url:
            return
        if any(job.url == url for job in self.jobs):
            self.notify("That URL is already in the queue.", severity="warning")
            return
        job = Job(uid=self._next_uid, url=url)
        self._next_uid += 1
        self.jobs.append(job)
        self.refresh_queue()
        self.focused_uid = job.uid
        self.queue().move_cursor(row=len(self.jobs) - 1)
        self.refresh_focused()
        self.analyze_job(job)

    def add_job_from_info(self, url: str, info: dict) -> Job | None:
        if any(job.url == url for job in self.jobs):
            return None
        job = Job(uid=self._next_uid, url=url)
        self._next_uid += 1
        self.jobs.append(job)
        self.absorb_info(job, info)
        self.refresh_queue()
        if self.focused_uid is None:
            self.focused_uid = job.uid
            self.queue().move_cursor(row=0)
        self.refresh_focused()
        return job

    def absorb_info(self, job: Job, info: dict) -> None:
        if info.get("_type") == "playlist":
            entries = info.get("entries") or []
            job.status = Status.ERROR
            job.error = f"playlist with {len(entries)} entries (not supported yet)"
            job.title = info.get("title") or job.url
            self.post_message(JobChanged(job.uid))
            return
        from model import parse_formats

        job.title = info.get("title") or info.get("id") or job.url
        job.uploader = info.get("uploader") or ""
        job.duration = info.get("duration")
        job.formats = parse_formats(info)
        if not job.video_id and not job.audio_id:
            job.video_id, job.audio_id = default_selection(job.formats)
        self.post_message(JobChanged(job.uid))

    @work(exclusive=False, group="analyze")
    async def analyze_job(self, job: Job) -> None:
        self.write_log(f"[dim]$ yt-dlp -J --flat-playlist {job.url}[/dim]")
        try:
            info, warnings = await run_json(job.url, flat=True)
        except YtdlpError as exc:
            job.status = Status.ERROR
            job.error = str(exc)[:110]
            self.post_message(JobChanged(job.uid))
            self.notify(f"Could not analyze: {job.error}", severity="error")
            return
        if warnings:
            self.write_log(f"[yellow]yt-dlp warned:[/yellow] {warnings.splitlines()[-1][:150]}")
        self.absorb_info(job, info)
        if job.status is Status.ERROR:
            self.notify(job.error, severity="error")

    # ------------------------------------------------------------- focus sync
    def on_data_table_row_highlighted(self, event: DataTable.RowHighlighted) -> None:
        if event.data_table.id != "queue":
            return
        try:
            self.focused_uid = int(event.row_key.value)
        except (TypeError, ValueError):
            return
        self.refresh_focused()

    def refresh_focused(self) -> None:
        self.refresh_report()
        self.refresh_formats()
        self.refresh_slots()

    def refresh_report(self) -> None:
        job = self.focused_job()
        if job is None:
            self.report_text = "Paste a URL to start a queue."
        elif not job.analyzed:
            self.report_text = "Formats not fetched. Press `e` to analyze this row."
        else:
            lines = availability_report(job.formats, self.merge_format)
            self.report_text = "\n".join(lines) if lines else "No formats."
        try:
            self.query_one("#report", Static).update(self.report_text)
        except Exception:
            pass

    def refresh_formats(self) -> None:
        job = self.focused_job()
        self._visible = (
            apply_filters(job.formats, **self.current_filters()) if job else []
        )
        table = self.formats_table()
        cursor = table.cursor_row
        table.clear()
        for fmt in self._visible:
            table.add_row(*self.format_cells(job, fmt), key=fmt.id)
        if self._visible:
            table.move_cursor(row=min(cursor, len(self._visible) - 1))

    def format_cells(self, job: Job, fmt) -> list:
        style = KIND_STYLE.get(fmt.kind, "")
        mark = ""
        if job and fmt.id == job.video_id:
            mark = "V"
        if job and fmt.id == job.audio_id:
            mark = "A" if not mark else "V+A"
        note = fmt.note or ""
        if fmt.language:
            note = f"{note} [{fmt.language}]".strip()
        from model import human_bitrate, human_size

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

    def refresh_slots(self) -> None:
        job = self.focused_job()
        if job is None:
            text = "[dim]no row selected[/dim]"
        else:
            selector = job.selector() or "auto (config default)"
            text = (
                f"[bold]{job.label(46)}[/bold]  ·  "
                f"calidad: {job.quality_label()}  ·  audio: {job.audio_label()}  ·  "
                f"-f {selector}  ·  saves to {self.outdir}"
            )
            if job.video() is not None and job.video().kind == "video" and not job.audio_id:
                text += "  [yellow]⚠ no audio selected[/yellow]"
        try:
            self.query_one("#slots", Static).update(text)
        except Exception:
            pass

    def current_filters(self) -> dict:
        return {
            "text": self.query_one("#filter", Input).value,
            "kind": self.query_one("#kind", Select).value or "all",
            "container": self.query_one("#container", Select).value or "all",
            "only_with_size": self.query_one("#only_size", Checkbox).value,
            "include_hls": self.query_one("#hls", Checkbox).value,
        }

    def on_input_changed(self, event: Input.Changed) -> None:
        if event.input.id == "filter":
            self.refresh_formats()

    def on_select_changed(self, event: Select.Changed) -> None:
        self.refresh_formats()

    def on_checkbox_changed(self, event: Checkbox.Changed) -> None:
        self.refresh_formats()

    def on_job_changed(self, message: JobChanged) -> None:
        try:
            self.update_row(message.uid)
            if message.uid == self.focused_uid:
                self.refresh_slots()
        except NoMatches:
            return  # the app is shutting down

    def on_unmount(self) -> None:
        if self.manager is not None:
            self.manager.kill_all()

    # ------------------------------------------------------------------ input
    def on_input_submitted(self, event: Input.Submitted) -> None:
        if event.input.id != "urlbar":
            return
        raw = event.value.strip()
        if not raw:
            return
        event.input.value = ""
        for url in raw.split():
            self.add_url(url)

    # -------------------------------------------------------------- selection
    def current_format_id(self) -> str | None:
        if not self._visible:
            return None
        index = self.formats_table().cursor_row
        if 0 <= index < len(self._visible):
            return self._visible[index].id
        return None

    def pick_video(self, fmt_id: str | None, uid: int | None = None) -> bool:
        job = self.job_by_uid(uid if uid is not None else self.focused_uid)
        fmt = job.find(fmt_id) if job is not None and fmt_id else None
        if fmt is None or fmt.kind not in ("video", "muxed"):
            return False
        job.video_id = fmt.id
        if fmt.kind == "muxed":
            job.audio_id = None
        self._choice_changed(job)
        return True

    def pick_audio(self, fmt_id: str | None, uid: int | None = None) -> bool:
        job = self.job_by_uid(uid if uid is not None else self.focused_uid)
        fmt = job.find(fmt_id) if job is not None and fmt_id else None
        if fmt is None or fmt.kind != "audio":
            return False
        job.audio_id = fmt.id
        self._choice_changed(job)
        return True

    def _choice_changed(self, job: Job) -> None:
        self.update_row(job.uid)
        if job.uid == self.focused_uid:
            self.refresh_formats()
            self.refresh_slots()

    def action_pick_video(self) -> None:
        if not self.pick_video(self.current_format_id()):
            self.notify("That row is not a video format.", severity="warning")

    def action_pick_audio(self) -> None:
        if not self.pick_audio(self.current_format_id()):
            self.notify("That row is not an audio-only format.", severity="warning")

    def visible_ids(self) -> list[str]:
        return [fmt.id for fmt in self._visible]

    def row_count(self) -> int:
        return self.formats_table().row_count

    def action_reset_choice(self) -> None:
        job = self.focused_job()
        if job is None:
            return
        job.video_id, job.audio_id = default_selection(job.formats)
        self.update_row(job.uid)
        self.refresh_formats()
        self.refresh_slots()

    def action_edit_job(self) -> None:
        job = self.focused_job()
        if job is None:
            return
        if job.analyzed:
            self.notify(f"{len(job.formats)} formats already loaded — pick with v/a.")
            return
        self.analyze_job(job)

    # --------------------------------------------------------------- download
    def action_download(self) -> None:
        job = self.focused_job()
        if job is None or self.manager is None:
            self.notify("Nothing selected.", severity="warning")
            return
        if job.status is Status.DOWNLOADING:
            self.notify("Already downloading.", severity="warning")
            return
        added = self.manager.submit([job])
        if added:
            self.show_log()
            self.write_log(f"[dim]queued {job.label(50)}[/dim]")

    def action_start_all(self) -> None:
        if self.manager is None:
            return
        pending = [
            job
            for job in self.jobs
            if job.status in (Status.LOADED, Status.ERROR, Status.CANCELLED)
        ]
        if not pending:
            self.notify("Nothing pending.")
            return
        added = self.manager.submit(pending)
        self.show_log()
        self.write_log(
            f"[dim]queued {added} job(s), concurrency {self.manager.max_concurrent}[/dim]"
        )

    def action_cancel_job(self) -> None:
        job = self.focused_job()
        if job is None or self.manager is None or job.status is not Status.DOWNLOADING:
            self.notify("Nothing to cancel.", severity="warning")
            return
        job.status = Status.CANCELLED
        self.manager.cancel(job.uid)
        self.update_row(job.uid)

    def action_delete_job(self) -> None:
        job = self.focused_job()
        if job is None:
            return
        if job.status is Status.DOWNLOADING and self.manager is not None:
            job.status = Status.CANCELLED
            self.manager.cancel(job.uid)
        self.jobs.remove(job)
        self.focused_uid = self.jobs[0].uid if self.jobs else None
        self.refresh_queue()
        self.refresh_focused()

    def action_focus_filter(self) -> None:
        self.query_one("#filter", Input).focus()

    def action_toggle_log(self) -> None:
        self.query_one("#log", RichLog).toggle_class("visible")
