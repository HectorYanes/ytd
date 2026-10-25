#!/usr/bin/env python3
"""Tests for ytd. Run: uv run --with textual python test_ytd.py"""

from __future__ import annotations

import asyncio
import inspect
import sys
import traceback

from ytd import (
    YtdApp,
    apply_filters,
    availability_report,
    build_format_selector,
    child_env,
    codec_name,
    human_size,
    parse_formats,
    parse_progress,
)

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


async def test_app_layout_survives_short_terminal():
    """Regression: fixed heights used to collapse the table to 0 rows at 80x24."""
    app = YtdApp(info=INFO)
    async with app.run_test(size=(80, 24)) as pilot:
        await pilot.pause()
        assert app.row_count() > 0
        assert app.table().size.height >= 3


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


async def test_app_renders_all_and_filters_rows():
    app = YtdApp(info=INFO)
    async with app.run_test() as pilot:
        await pilot.pause()
        assert app.row_count() == len(INFO["formats"]) - 1  # storyboard hidden by default
        app.query_one("#filter").value = "1080"
        await pilot.pause()
        assert app.row_count() == 3
        assert app.visible_ids() == ["299", "303", "301"]


async def test_app_selection_builds_selector():
    app = YtdApp(info=INFO)
    async with app.run_test() as pilot:
        await pilot.pause()
        app.select_video("299")
        app.select_audio("140")
        await pilot.pause()
        assert app.selector() == "299+140"
        app.select_video("18")
        await pilot.pause()
        assert app.selector() == "18"
        assert app.selected_audio is None


async def test_app_rejects_wrong_kind():
    app = YtdApp(info=INFO)
    async with app.run_test() as pilot:
        await pilot.pause()
        assert app.select_audio("299") is False
        assert app.select_video("140") is False


async def test_app_command_line_uses_ytdlp_and_respects_dir():
    app = YtdApp(info=INFO, outdir="/tmp/opencode/x")
    async with app.run_test() as pilot:
        await pilot.pause()
        app.select_video("299")
        app.select_audio("251")
        cmd = app.download_command()
        assert cmd[0].endswith("yt-dlp") or cmd[0] == "yt-dlp"
        assert "-f" in cmd and "299+251" in cmd
        assert "/tmp/opencode/x" in " ".join(cmd)


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
