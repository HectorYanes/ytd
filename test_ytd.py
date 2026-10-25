#!/usr/bin/env python3
"""Tests for ytd. Run: uv run --with textual python test_ytd.py"""

from __future__ import annotations

import asyncio
import inspect
import sys
import traceback

import app as app_module
from core import (
    DownloadManager,
    YtdlpError,
    decode_json,
    child_env,
    download_command,
    parse_progress,
)
from model import (
    Fmt,
    Job,
    Status,
    compatibility_note,
    is_compatible,
    apply_filters,
    availability_report,
    build_format_selector,
    codec_name,
    default_selection,
    human_size,
    parse_formats,
    status_label,
)
from ytd import YtdApp

# Fixture shaped after real `yt-dlp -J` output (Big Buck Bunny 4K probe).
INFO = {
    "_type": "video",
    "id": "aqz-KE-bpKQ",
    "title": "Big Buck Bunny 60fps 4K",
    "uploader": "Blender",
    "duration": 635,
    "is_live": False,
    "webpage_url": "https://www.youtube.com/watch?v=aqz-KE-bpKQ",
    "formats": [
        {"format_id": "sb0", "ext": "mhtml", "vcodec": "none", "acodec": "none",
         "height": 180, "fps": 0.2, "protocol": "mhtml", "format_note": "storyboard"},
        {"format_id": "140", "ext": "m4a", "vcodec": "none", "acodec": "mp4a.40.2",
         "abr": 129.5, "tbr": 129.5, "filesize": 10271496, "protocol": "https",
         "format_note": "medium"},
        {"format_id": "251", "ext": "webm", "vcodec": "none", "acodec": "opus",
         "abr": 128.6, "tbr": 128.6, "filesize": 10202210, "protocol": "https",
         "format_note": "medium"},
        {"format_id": "18", "ext": "mp4", "vcodec": "avc1.42001E", "acodec": "mp4a.40.2",
         "height": 360, "width": 640, "fps": 30, "tbr": 359.6,
         "filesize": 28526904, "protocol": "https", "format_note": "360p",
         "dynamic_range": "SDR"},
        {"format_id": "299", "ext": "mp4", "vcodec": "avc1.64002A", "acodec": "none",
         "height": 1080, "width": 1920, "fps": 60, "tbr": 3247.8,
         "filesize": 257619653, "protocol": "https", "format_note": "1080p60",
         "dynamic_range": "SDR"},
        {"format_id": "303", "ext": "webm", "vcodec": "vp9", "acodec": "none",
         "height": 1080, "width": 1920, "fps": 60, "tbr": 2127.2,
         "filesize": 168736189, "protocol": "https", "format_note": "1080p60",
         "dynamic_range": "SDR"},
        {"format_id": "401", "ext": "mp4", "vcodec": "av01.0.13M.0", "acodec": "none",
         "height": 2160, "width": 3840, "fps": 60, "tbr": 8981.8,
         "filesize_approx": 712445280, "protocol": "https", "format_note": "2160p60",
         "dynamic_range": "HDR10"},
        {"format_id": "301", "ext": "mp4", "vcodec": "avc1.64002A", "acodec": "mp4a.40.2",
         "height": 1080, "fps": 60, "tbr": 7417.7, "protocol": "m3u8_native",
         "dynamic_range": "SDR"},
    ],
}


def test_codec_name_mapping():
    cases = {
        "avc1.64002A": "h264",
        "av01.0.13M.0": "av1",
        "vp9": "vp9",
        "vp09.00.10.08": "vp9",
        "hev1.1.6.L93": "h265",
        "hvc1.2.4": "h265",
        "av01": "av1",
        "mp4a.40.2": "aac",
        "opus": "opus",
        "ec-3": "eac3",
        "ac-3": "ac3",
        "none": None,
        None: None,
    }
    for raw, expected in cases.items():
        got = codec_name(raw)
        assert got == expected, f"{raw!r} -> {got!r}, expected {expected!r}"


def test_parse_formats_classifies_kinds():
    fmts = {f.id: f for f in parse_formats(INFO)}
    assert fmts["sb0"].kind == "image"
    assert fmts["sb0"].available is False
    assert fmts["140"].kind == "audio"
    assert fmts["140"].available is True
    assert fmts["299"].kind == "video"
    assert fmts["18"].kind == "muxed"
    assert fmts["301"].is_hls is True
    assert fmts["299"].is_hls is False


def test_parse_formats_resolution_and_labels():
    fmts = {f.id: f for f in parse_formats(INFO)}
    assert fmts["299"].resolution == "1080p60"
    assert fmts["18"].resolution == "360p"
    assert fmts["140"].resolution == "audio"
    assert fmts["140"].vcodec_name is None
    assert fmts["140"].acodec_name == "aac"
    assert fmts["401"].hdr == "HDR10"


def test_size_uses_approx_when_missing():
    fmts = {f.id: f for f in parse_formats(INFO)}
    assert fmts["140"].size == 10271496
    assert fmts["401"].size == 712445280


def test_human_size():
    assert human_size(None) == "—"
    assert human_size(0) == "0 B"
    assert human_size(1023) == "1023 B"
    assert human_size(1024) == "1.0 KiB"
    assert human_size(150524867) == "143.6 MiB"


def test_apply_filters_by_kind():
    fmts = parse_formats(INFO)
    vids = apply_filters(fmts, kind="video")
    assert {f.kind for f in vids} == {"video"}
    assert "299" in {f.id for f in vids}

    audio = apply_filters(fmts, kind="audio")
    assert {f.kind for f in audio} == {"audio"}

    muxed = apply_filters(fmts, kind="muxed")
    assert "18" in {f.id for f in muxed}


def test_apply_filters_hides_images_by_default():
    fmts = parse_formats(INFO)
    default = apply_filters(fmts)
    assert all(f.kind != "image" for f in default)
    shown = apply_filters(fmts, show_images=True)
    assert any(f.kind == "image" for f in shown)


def test_apply_filters_by_free_text():
    fmts = parse_formats(INFO)
    assert [f.id for f in apply_filters(fmts, text="1080")] == ["299", "303", "301"]
    assert {f.id for f in apply_filters(fmts, text="av1")} == {"401"}
    assert {f.id for f in apply_filters(fmts, text="opus")} == {"251"}
    assert apply_filters(fmts, text="4080") == []


def test_apply_filters_by_container_and_size():
    fmts = parse_formats(INFO)
    m4a = apply_filters(fmts, container="m4a")
    assert {f.id for f in m4a} == {"140"}
    sized = apply_filters(fmts, only_with_size=True)
    assert all(f.size for f in sized)
    assert "301" not in {f.id for f in sized}


def test_apply_filters_ignores_hls_when_asked():
    fmts = parse_formats(INFO)
    progressive = apply_filters(fmts, include_hls=False)
    assert "301" not in {f.id for f in progressive}


def test_availability_report_flags_video_only_max_quality():
    report = availability_report(parse_formats(INFO), merge_format="mp4")
    text = "\n".join(report)
    assert "2160p" in text
    assert "video-only" in text.lower()
    assert "av1" in text.lower()


def test_availability_report_warns_opus_in_mp4():
    report = availability_report(parse_formats(INFO), merge_format="mp4")
    text = "\n".join(report).lower()
    assert "opus" in text and "mp4" in text


def test_availability_report_lists_audio():
    report = availability_report(parse_formats(INFO), merge_format="mp4")
    text = "\n".join(report).lower()
    assert "aac" in text and "opus" in text


def test_child_env_drops_uv_interpreter():
    fake = {
        "PATH": "/venv/bin:/home/u/.cache/uv/archive-v0/x/bin:/home/u/.local/share/uv/python/bin:/usr/bin:/bin",
        "VIRTUAL_ENV": "/venv",
        "PYTHONPATH": "/venv/lib",
        "HOME": "/home/u",
    }
    env = child_env(fake)
    assert env["PATH"] == "/usr/bin:/bin"
    assert "VIRTUAL_ENV" not in env
    assert "PYTHONPATH" not in env
    assert env["HOME"] == "/home/u"


URL = INFO["webpage_url"]
PLAYLIST_URL = "https://www.youtube.com/playlist?list=PLtest"
PLAYLIST = {
    "_type": "playlist",
    "id": "PLtest",
    "title": "Test list",
    "uploader": "Someone",
    "entries": [
        {"_type": "url", "id": "aaa", "url": "https://www.youtube.com/watch?v=aaa",
         "title": "One", "duration": 100, "availability": "public"},
        {"_type": "url", "id": "bbb", "url": "https://www.youtube.com/watch?v=bbb",
         "title": "Two", "duration": 200, "availability": "public"},
        {"_type": "url", "id": "ccc", "url": "https://www.youtube.com/watch?v=ccc",
         "title": "Gone", "availability": "private"},
        None,
    ],
}


# --------------------------------------------------------------------------- #
# fakes
# --------------------------------------------------------------------------- #
class FakeProc:
    """Stands in for an asyncio subprocess running yt-dlp."""

    def __init__(self, lines=(), code=0, hold=None):
        self._lines = [line.encode() for line in lines]
        self._code = code
        self._hold = hold
        self.terminated = False
        self.stdout = self

    async def readline(self):
        if self.terminated:
            return b""
        if self._lines:
            return self._lines.pop(0) + b"\n"
        while self._hold is not None and not self._hold.is_set():
            if self.terminated:
                return b""
            try:
                await asyncio.wait_for(asyncio.shield(self._hold.wait()), 0.01)
            except asyncio.TimeoutError:
                pass
        return b""

    async def wait(self):
        await asyncio.sleep(0)
        return -15 if self.terminated else self._code

    def terminate(self):
        self.terminated = True


def spawner(proc_factory, seen=None):
    async def spawn(cmd):
        if seen is not None:
            seen.append(cmd)
        return proc_factory()

    return spawn


async def wait_until(predicate, timeout=3.0):
    loop = asyncio.get_running_loop()
    end = loop.time() + timeout
    while loop.time() < end:
        if predicate():
            return True
        await asyncio.sleep(0.01)
    return False


async def pump(pilot, predicate, timeout=3.0):
    loop = asyncio.get_running_loop()
    end = loop.time() + timeout
    while loop.time() < end:
        if predicate():
            return True
        await pilot.pause(0.02)
    return False


def test_build_format_selector():
    assert build_format_selector("299", "140") == "299+140"
    assert build_format_selector("18", None) == "18"
    assert build_format_selector(None, "140") == "140"
    assert build_format_selector(None, None) == ""


def test_parse_progress_line():
    got = parse_progress("[download]  45.2% of  1.32GiB at    2.10MiB/s ETA 00:12")
    assert got is not None
    assert abs(got["percent"] - 45.2) < 1e-9
    assert got["size"] == "1.32GiB"
    assert got["speed"] == "2.10MiB/s"
    assert got["eta"] == "00:12"


def test_parse_progress_finished_variant():
    got = parse_progress("[download] 100% of    9.80MiB in 00:00:08 at 1.18MiB/s")
    assert got is not None
    assert got["percent"] == 100
    assert got["size"] == "9.80MiB"
    assert got["speed"] == "1.18MiB/s"
    assert got["eta"] is None


def test_parse_progress_ignores_other_lines():
    assert parse_progress('[download] Destination: /tmp/a.mp4') is None
    assert parse_progress('[Merger] Merging formats into "/tmp/a.mp4"') is None
    assert parse_progress('') is None


# --------------------------------------------------------------------------- #
# queue model
# --------------------------------------------------------------------------- #
def test_status_label_variants():
    assert status_label(Status.LOADED) == "cargada"
    assert status_label(Status.DOWNLOADING, 45.4) == "descargando (45%)"
    assert status_label(Status.DONE, 100) == "completado"
    assert status_label(Status.ERROR, error="boom") == "error: boom"


def test_default_selection_picks_best_video_and_audio():
    formats = parse_formats(INFO)
    video_id, audio_id = default_selection(formats)
    assert video_id == "401"  # 2160p av1 video-only
    assert audio_id == "140"  # highest-bitrate audio


def test_default_selection_muxed_has_no_audio_pair():
    formats = parse_formats({"formats": [INFO["formats"][3]]})  # only format 18
    assert default_selection(formats) == ("18", None)


def test_job_labels_and_selector():
    job = Job(uid=1, url=URL)
    assert job.selector() == ""
    assert job.quality_label() == "auto (best)"
    assert job.audio_label() == "auto (best)"
    job.formats = parse_formats(INFO)
    job.video_id, job.audio_id = "299", "140"
    assert job.selector() == "299+140"
    assert job.quality_label() == "1080p60 · h264"
    assert job.audio_label() == "aac 130k"
    assert job.size_label() != "—"
    job.video_id, job.audio_id = "18", None
    assert job.selector() == "18"
    assert job.audio_label() == "— (incluido)"


def test_decode_json_rejects_null_payload():
    try:
        decode_json("null", "ERROR: [youtube] x: This video is unavailable", 1)
    except YtdlpError as exc:
        assert "unavailable" in str(exc)
    else:
        raise AssertionError("expected YtdlpError")
    assert decode_json('{"_type": "video"}', "", 0) == {"_type": "video"}


def test_is_compatible_matrix():
    video = lambda codec: Fmt(id="v", ext="mp4", vcodec=codec, acodec=None, kind="video")
    audio = lambda codec: Fmt(id="a", ext="m4a", vcodec=None, acodec=codec, kind="audio")
    assert is_compatible(video("avc1.64002A"), "mp4") is True
    assert is_compatible(video("vp09.00.10.08"), "mp4") is False
    assert is_compatible(video("av01.0.13M.0"), "mp4") is True
    assert is_compatible(audio("mp4a.40.2"), "mp4") is True
    assert is_compatible(audio("opus"), "mp4") is False
    assert is_compatible(audio("opus"), "webm") is True
    assert is_compatible(video("vp09.00.10.08"), "mkv") is True
    assert is_compatible(Fmt(id="m", ext="mp4", vcodec="vp9", acodec="opus", kind="muxed"), "mp4") is True
    assert compatibility_note(audio("opus"), "mp4") == "opus not compatible with mp4"
    assert compatibility_note(audio("mp4a.40.2"), "mp4") == ""


def test_download_command_omits_format_when_auto():
    job = Job(uid=1, url=URL)
    cmd = download_command(job, "/tmp/out", binary="yt-dlp")
    assert cmd[:1] == ["yt-dlp"]
    assert "-f" not in cmd
    assert cmd[1:3] == ["-P", "home:/tmp/out"]
    assert cmd[-1] == URL
    job.video_id, job.audio_id = "299", "140"
    cmd = download_command(job, "/tmp/out", binary="yt-dlp")
    assert cmd[1:3] == ["-f", "299+140"]


# --------------------------------------------------------------------------- #
# download manager
# --------------------------------------------------------------------------- #
async def test_manager_reports_progress_then_done():
    hold = asyncio.Event()
    proc = FakeProc(
        [
            "[download]  45.2% of  1.32GiB at    2.10MiB/s ETA 00:12",
            "[download] Destination: /tmp/out/a.mp4",
        ],
        hold=hold,
    )
    updates = []
    manager = DownloadManager(
        outdir="/tmp/out", max_concurrent=1,
        on_update=lambda job: updates.append((job.status, job.percent)),
        spawn=spawner(lambda: proc),
    )
    manager.start()
    job = Job(uid=1, url=URL)
    assert manager.submit([job]) == 1
    assert await wait_until(lambda: job.status is Status.DOWNLOADING and job.percent)
    assert job.percent == 45.2
    assert job.speed == "2.10MiB/s"
    assert job.path == "/tmp/out/a.mp4"
    hold.set()
    assert await wait_until(lambda: job.status is Status.DONE)
    assert job.percent == 100
    assert any(status is Status.DOWNLOADING and pct == 45.2 for status, pct in updates)
    await manager.stop()


async def test_manager_reports_error_line():
    manager = DownloadManager(
        outdir="/tmp/out", max_concurrent=1,
        spawn=spawner(lambda: FakeProc(["ERROR: [youtube] xyz: Private video"], code=1)),
    )
    manager.start()
    job = Job(uid=1, url=URL)
    manager.submit([job])
    assert await wait_until(lambda: job.status is Status.ERROR)
    assert job.error == "[youtube] xyz: Private video"
    await manager.stop()


async def test_manager_cancel_keeps_cancelled_status():
    hold = asyncio.Event()
    manager = DownloadManager(
        outdir="/tmp/out", max_concurrent=1, spawn=spawner(lambda: FakeProc(hold=hold)),
    )
    manager.start()
    job = Job(uid=1, url=URL)
    manager.submit([job])
    assert await wait_until(lambda: job.status is Status.DOWNLOADING)
    job.status = Status.CANCELLED
    assert manager.cancel(job.uid) is True
    assert await wait_until(lambda: manager.running_count == 0)
    assert job.status is Status.CANCELLED
    await manager.stop()


async def test_manager_caps_concurrency():
    holds = [asyncio.Event() for _ in range(4)]
    procs = iter([FakeProc(hold=hold) for hold in holds])
    manager = DownloadManager(
        outdir="/tmp/out", max_concurrent=2, spawn=spawner(lambda: next(procs)),
    )
    manager.start()
    jobs = [Job(uid=i + 1, url=f"https://example.com/{i}") for i in range(4)]
    assert manager.submit(jobs) == 4
    assert await wait_until(lambda: manager.running_count == 2)
    assert manager.max_observed == 2
    for hold in holds:
        hold.set()
    assert await wait_until(lambda: all(j.status is Status.DONE for j in jobs))
    assert manager.max_observed == 2
    await manager.stop()


async def test_manager_dedupes_and_retries():
    manager = DownloadManager(
        outdir="/tmp/out", max_concurrent=1,
        spawn=spawner(lambda: FakeProc(["ERROR: nope"], code=1)),
    )
    manager.start()
    job = Job(uid=1, url=URL)
    assert manager.submit([job]) == 1
    assert manager.submit([job]) == 0
    assert await wait_until(lambda: job.status is Status.ERROR)
    assert manager.submit([job]) == 1
    assert await wait_until(lambda: manager.running_count == 0)
    await manager.stop()


# --------------------------------------------------------------------------- #
# app
# --------------------------------------------------------------------------- #
async def test_app_layout_survives_short_terminal():
    """Regression: fixed heights used to collapse the table to 0 rows at 80x24."""
    app = YtdApp(seed=[(URL, INFO)])
    async with app.run_test(size=(80, 24)) as pilot:
        await pilot.pause()
        assert app.queue().row_count == 1
        assert app.queue().size.height >= 3
        assert app.row_count() > 0


async def test_app_renders_all_and_filters_rows():
    app = YtdApp(seed=[(URL, INFO)])
    async with app.run_test() as pilot:
        await pilot.pause()
        assert app.row_count() == len(INFO["formats"]) - 1  # storyboard hidden
        app.query_one("#filter").value = "1080"
        await pilot.pause()
        assert app.row_count() == 3
        assert app.visible_ids() == ["299", "303", "301"]


async def test_app_selection_builds_selector():
    app = YtdApp(seed=[(URL, INFO)])
    async with app.run_test() as pilot:
        await pilot.pause()
        job = app.jobs[0]
        assert app.pick_video("299") is True
        assert app.pick_audio("140") is True
        await pilot.pause()
        assert job.selector() == "299+140"
        assert app.pick_video("18") is True
        assert job.selector() == "18"
        assert job.audio_id is None


async def test_app_rejects_wrong_kind():
    app = YtdApp(seed=[(URL, INFO)])
    async with app.run_test() as pilot:
        await pilot.pause()
        assert app.pick_audio("299") is False
        assert app.pick_video("140") is False


async def test_app_analyze_loads_formats():
    original = app_module.run_json
    calls = []

    async def fake(url, *, flat=False, timeout=180):
        calls.append((url, flat))
        return INFO, ""

    app_module.run_json = fake
    try:
        app = YtdApp()
        async with app.run_test() as pilot:
            await pilot.pause()
            app.add_url(URL)
            assert await pump(pilot, lambda: app.jobs and app.jobs[0].analyzed)
            job = app.jobs[0]
            assert calls == [(URL, True)]
            assert job.title == INFO["title"]
            assert app.queue().row_count == 1
            assert str(app.queue().get_cell(str(job.uid), "status")) == "cargada"
            assert app.row_count() > 0
    finally:
        app_module.run_json = original


async def test_app_analysis_fills_the_explorer_of_the_focused_row():
    original = app_module.run_json

    async def fake(url, *, flat=False, timeout=180):
        return INFO, ""

    app_module.run_json = fake
    try:
        app = YtdApp()
        async with app.run_test() as pilot:
            await pilot.pause()
            app.add_url(URL)
            assert await pump(pilot, lambda: app.jobs and app.jobs[0].analyzed)
            await pilot.pause()
            assert app.row_count() > 0
            assert "2160p" in app.report_text
            assert app.focused_job().selector() != ""
    finally:
        app_module.run_json = original


async def test_app_analyze_failure_marks_error():
    original = app_module.run_json

    async def fake(url, *, flat=False, timeout=180):
        raise YtdlpError("Video unavailable")

    app_module.run_json = fake
    try:
        app = YtdApp()
        async with app.run_test() as pilot:
            await pilot.pause()
            app.add_url(URL)
            assert await pump(pilot, lambda: app.jobs and app.jobs[0].status is Status.ERROR)
            job = app.jobs[0]
            assert "Video unavailable" in job.error
            assert str(app.queue().get_cell(str(job.uid), "status")).startswith("error:")
    finally:
        app_module.run_json = original


async def test_app_expands_playlist_into_rows():
    original = app_module.run_json

    async def fake(url, *, flat=False, timeout=180):
        return PLAYLIST, ""

    app_module.run_json = fake
    try:
        app = YtdApp()
        async with app.run_test() as pilot:
            await pilot.pause()
            app.add_url(PLAYLIST_URL)
            assert await pump(pilot, lambda: len(app.jobs) == 3)
            assert [job.title for job in app.jobs] == ["One", "Two", "Gone"]
            assert all(job.url.startswith("https://www.youtube.com/watch?v=") for job in app.jobs)
            assert app.jobs[2].status is Status.ERROR
            assert "private" in app.jobs[2].error
            assert app.jobs[0].status is Status.LOADED
            assert not any(job.formats for job in app.jobs)  # lazy
            assert app.queue().row_count == 3
            assert app.focused_uid == app.jobs[0].uid
    finally:
        app_module.run_json = original


async def test_app_playlist_skips_urls_already_queued():
    original = app_module.run_json

    async def fake(url, *, flat=False, timeout=180):
        return PLAYLIST, ""

    app_module.run_json = fake
    try:
        existing = {"_type": "video", "title": "Already here", "formats": []}
        app = YtdApp(seed=[("https://www.youtube.com/watch?v=aaa", existing)])
        async with app.run_test() as pilot:
            await pilot.pause()
            app.add_url(PLAYLIST_URL)
            assert await pump(pilot, lambda: len(app.jobs) == 3)
            assert [job.title for job in app.jobs] == ["Already here", "Two", "Gone"]
    finally:
        app_module.run_json = original


async def test_app_add_url_dedupes():
    original = app_module.run_json

    async def fake(url, *, flat=False, timeout=180):
        return INFO, ""

    app_module.run_json = fake
    try:
        app = YtdApp(seed=[(URL, INFO)])
        async with app.run_test() as pilot:
            await pilot.pause()
            app.add_url(URL)
            await pilot.pause()
            assert len(app.jobs) == 1
            assert app.queue().row_count == 1
    finally:
        app_module.run_json = original


async def test_app_download_updates_row_status():
    hold = asyncio.Event()
    proc = FakeProc(["[download]  45.2% of  1.32GiB at 2.10MiB/s ETA 00:12"], hold=hold)
    app = YtdApp(seed=[(URL, INFO)], spawn=spawner(lambda: proc))
    async with app.run_test() as pilot:
        await pilot.pause()
        job = app.jobs[0]
        app.action_download()
        assert await pump(pilot, lambda: job.percent > 0)
        cell = str(app.queue().get_cell(str(job.uid), "status"))
        assert cell == "descargando (45%)"
        hold.set()
        assert await pump(pilot, lambda: job.status is Status.DONE)
        assert str(app.queue().get_cell(str(job.uid), "status")) == "completado"


async def test_app_compat_toggle_hides_incompatible_formats():
    app = YtdApp(seed=[(URL, INFO)], merge_format="mp4")
    async with app.run_test() as pilot:
        await pilot.pause()
        assert app.visible_ids() == ["140", "251", "18", "299", "303", "401", "301"]
        app.action_toggle_compat()
        await pilot.pause()
        assert app.visible_ids() == ["140", "18", "299", "401", "301"]
        assert app.compatible_only is True
        app.action_toggle_compat()
        await pilot.pause()
        assert app.row_count() == 7


async def test_app_format_note_flags_incompatible_container():
    app = YtdApp(seed=[(URL, INFO)], merge_format="mp4")
    async with app.run_test() as pilot:
        await pilot.pause()
        note_opus = str(app.formats_table().get_cell("251", "note"))
        note_aac = str(app.formats_table().get_cell("140", "note"))
        assert "not compatible" in note_opus
        assert "not compatible" not in note_aac


async def test_app_editor_opens_with_compatible_options_only():
    app = YtdApp(seed=[(URL, INFO)], merge_format="mp4")
    async with app.run_test(size=(80, 24)) as pilot:
        await pilot.pause()
        app.action_edit_job()
        await pilot.pause()
        editor = app.screen
        assert isinstance(editor, app_module.JobEditor)
        assert editor.size.width <= 80 and editor.size.height <= 24
        video_values = [value for _, value in editor.video_options()]
        audio_values = [value for _, value in editor.audio_options()]
        assert "299" in video_values
        assert "303" not in video_values  # vp9 in mp4
        assert "140" in audio_values
        assert "251" not in audio_values  # opus in mp4
        editor.show_incompatible = True
        editor.rebuild()
        assert "303" in [value for _, value in editor.video_options()]
        editor.dismiss(("299", "140"))
        await pilot.pause()
        assert app.jobs[0].selector() == "299+140"
        assert app.focused_job() is app.jobs[0]


async def test_app_editor_muxed_choice_clears_audio():
    app = YtdApp(seed=[(URL, INFO)], merge_format="mp4")
    async with app.run_test() as pilot:
        await pilot.pause()
        job = app.jobs[0]
        app.apply_editor(job, ("18", "140"))
        await pilot.pause()
        assert job.selector() == "18"
        assert job.audio_id is None
        app.apply_editor(job, None)  # cancelled editor changes nothing
        assert job.selector() == "18"


async def test_app_key_bindings_do_not_crash():
    hold = asyncio.Event()
    app = YtdApp(
        seed=[(URL, INFO)], spawn=spawner(lambda: FakeProc(hold=hold)), merge_format="mp4"
    )
    async with app.run_test(size=(100, 30)) as pilot:
        await pilot.pause()
        app.queue().focus()
        for key in ("down", "up", "v", "a", "r", "c", "c", "l", "l"):
            await pilot.press(key)
            await pilot.pause()
        await pilot.press("e")
        await pilot.pause()
        assert isinstance(app.screen, app_module.JobEditor)
        await pilot.press("escape")
        await pilot.pause()
        assert app.screen is app.screen_stack[0]
        app.action_start_all()
        await pilot.pause()
        await pilot.press("x")
        await pilot.pause()
        await pilot.press("delete")
        await pilot.pause()
        assert app.jobs == []
    hold.set()


async def test_app_delete_and_cancel_bindings():
    hold = asyncio.Event()
    app = YtdApp(
        seed=[(URL, INFO)], spawn=spawner(lambda: FakeProc(hold=hold)),
    )
    async with app.run_test() as pilot:
        await pilot.pause()
        job = app.jobs[0]
        app.action_download()
        assert await pump(pilot, lambda: job.status is Status.DOWNLOADING)
        app.action_cancel_job()
        assert job.status is Status.CANCELLED
        app.action_delete_job()
        assert app.jobs == []
        assert app.queue().row_count == 0


def main() -> int:
    tests = [(n, f) for n, f in sorted(globals().items()) if n.startswith("test_")]
    failed = 0
    for name, fn in tests:
        try:
            if inspect.iscoroutinefunction(fn):
                asyncio.run(fn())
            else:
                fn()
        except Exception:
            failed += 1
            print(f"FAIL  {name}")
            traceback.print_exc()
        else:
            print(f"PASS  {name}")
    print(f"\n{len(tests) - failed}/{len(tests)} passed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
