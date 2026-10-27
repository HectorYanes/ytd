# ytd — a queue TUI for yt-dlp

Paste one link or twenty. Each one becomes a row with the quality it will
download, the audio that goes with it, and a live status. The format table
below is for the focused row only, so you see 8 filters and one video instead
of `yt-dlp -F`'s wall of 47 rows.

Runs on Linux, macOS and Windows with the same keys and the same queue.

## Install

You need [uv](https://docs.astral.sh/uv/) and — for merging video+audio and
embedding metadata — `ffmpeg`. Everything Python-side (yt-dlp, the JavaScript
runtime YouTube requires, Textual) is installed for you.

| system | install the prerequisites | then |
| --- | --- | --- |
| Windows (PowerShell) | `winget install --id astral-sh.uv -e`<br>`winget install --id Gyan.FFmpeg -e` | `uv tool install ytd_tui-0.2.0-py3-none-any.whl` |
| macOS | `brew install uv ffmpeg` | `uv tool install ytd_tui-0.2.0-py3-none-any.whl` |
| Linux | `curl -LsSf https://astral.sh/uv/install.sh \| sh` and install `ffmpeg` | `uv tool install ytd_tui-0.2.0-py3-none-any.whl` |

### One command instead of those steps

`install.sh` (Linux/macOS) and `install.ps1` (Windows) do exactly the table
above: install `uv` and `ffmpeg` when they are missing, install this folder's
wheel as a tool, then run `ytd --check`.

```bash
./install.sh
```

```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

`install.ps1` also takes `-SkipFfmpeg` and `-SkipCheck`. If a `ytd` you put in
the tool directory by hand already exists, both scripts stop instead of
overwriting it (`YTD_ALLOW_OVERWRITE=1` / `$env:YTD_ALLOW_OVERWRITE=1` to
proceed).

A full install is about **150 MB** (92 MB of it is the deno runtime YouTube
needs; the Python side is ~55 MB). `ffmpeg` is a separate system package.

Open a fresh terminal after installing `uv`/`ffmpeg` so `PATH` picks them up.
From inside the source folder, `uv tool install .` does the same as the wheel.

```bash
ytd                                   # empty queue, paste URLs
ytd 'https://youtu.be/…' 'https://…'  # queue them on start
ytd -o ~/Videos -j 4 'URL'            # custom dir, 4 downloads at once
ytd --check                           # diagnose yt-dlp / ffmpeg / config
```

`pipx install .` works too. `ytd` gets its own environment, so it never
touches your system Python.

### Without installing anything

The folder also runs in place, provisioning its dependencies on first launch
(cached afterwards):

```powershell
.\ytd.cmd              # Windows
```

```bash
uv run ytd.py          # Linux / macOS
```

This path is slower to start the very first time and leaves the console
attached; installing as a tool is the better experience.

## The queue

| column | meaning |
| --- | --- |
| `#` | row order |
| `título` | video title (the URL until analyzed) |
| `calidad` | video stream that will be used, `auto (best)` when unnamed |
| `audio` | audio stream, or `— (incluido)` for muxed formats |
| `estado` | `cargada`, `descargando (45%)`, `completado`, `error: …`, `cancelado` |
| `tamaño` | expected size from the format metadata |

The queue lives on disk, so it survives closing the TUI:
`$XDG_STATE_HOME/ytd/queue.json` (`~/.local/state/ytd/queue.json` by default;
`%LOCALAPPDATA%` on Windows, `~/Library/Application Support` on macOS). Leaving
pauses whatever was downloading; coming back restores every row — formats and
choices included — and re-queues the paused downloads with `--continue`, so
yt-dlp picks up the `.part` file instead of starting over. A corrupt or foreign
queue file is ignored with a note in the log instead of blocking startup. Two
instances at once are not coordinated: the last one to save wins.

Pasting a link appends a row and analyzes it in the background
(`yt-dlp -J --flat-playlist`, one call handles both single videos and
playlists). Duplicate URLs are skipped with a notice. A failure marks only its
own row.

## Keys

| key | action |
| --- | --- |
| `Enter` in the URL field | queue the URL(s), space separated |
| `v` | set the highlighted format row as the **video** part |
| `a` | set the highlighted format row as the **audio** part |
| `e` | analyze the focused row, or open the format editor once it is analyzed |
| `d` | download the focused row |
| `s` | download every pending row |
| `x` | cancel the focused download |
| `Delete` | remove the focused row |
| `c` | show only formats the merge container accepts |
| `r` | reset the choice back to the automatic pick |
| `f` | jump to the filter box |
| `l` | toggle the log pane |
| `q` | quit |

Filtering is live: the free-text box matches id, extension, kind, resolution,
both codecs, HDR, notes and language. The dropdowns narrow by kind and
container; the checkboxes hide formats with no known size and hide HLS
variants. Storyboards are hidden unless you select "images".

## Why select two rows

Above 720p YouTube serves video and audio as separate streams — a "1080p MP4"
does not exist as a single file. `ytd` makes that explicit: `v` + `a` builds
`-f 137+140`, and the availability panel tells you which heights are muxed,
which are HLS-only, and which are video-only.

If you never pick, the row downloads with **no `-f` at all**, so your yt-dlp
config's `--format "bv*+ba/b"` decides.

`e` opens an editor with a video and an audio dropdown. It only lists streams
the merge container can hold: with `--merge-output-format mp4`, VP9 video and
Opus/Vorbis audio are hidden — muxing those into MP4 means a silent re-encode,
which is exactly the trap `yt-dlp -F` hides from you. `c` applies the same rule
to the explorer table, and any incompatible row is flagged in the `note`
column.

## Interaction with your yt-dlp config

Your config is still loaded (cookies, output template, merge container,
embedding). `ytd` only overrides two things:

- `-f` — your selection wins over `--format` in the config, and only when you
  selected something.
- `-P home:<dir>` — the output directory, verified to override a `--paths`
  entry from the config.

Where the config lives, per platform:

| system | paths checked, first hit wins |
| --- | --- |
| Windows | `%APPDATA%\yt-dlp\config`, `config.txt`, `%USERPROFILE%\yt-dlp.conf` |
| macOS | `~/Library/Application Support/yt-dlp/config`, `config.txt`, `~/yt-dlp.conf` |
| Linux | `$XDG_CONFIG_HOME/yt-dlp/config`, `config.txt`, `~/yt-dlp.conf`, `/etc/yt-dlp.conf` |

`ytd --check` prints the one it actually found.

## Which yt-dlp runs

Most specific first:

1. `YTD_YTDLP` — an explicit path you set.
2. `yt-dlp` on your `PATH` — your own install, with its own extras.
3. The copy bundled with this package (`python -m yt_dlp`), used when there is
   no system yt-dlp. This is the normal case on Windows after
   `uv tool install`.

The bundled copy also brings a **JavaScript runtime** (deno). YouTube returns no
formats at all without one — not even `-f 18` — so it is a hard dependency, and
`--check` reports where it was found. If you prefer your own, any of
`deno`, `node`, `bun` on `PATH` is used, and `YTD_YTDLP` points at a specific
yt-dlp binary.

`ffmpeg` is not bundled. Without it, muxed downloads such as `-f 18` still
work, but merging separate video+audio and embedding metadata/thumbnails fail;
`ytd --check` says so and the UI warns at startup.

## Troubleshooting

Start with:

```bash
ytd --check
```

```
ytd        0.2.0
python     3.14.0
platform   Linux 7.1.8-1-cachyos (x86_64)
yt-dlp     bundled module (/…/bin/python -m yt_dlp)
yt-dlp ver 2026.10.21
ffmpeg     /usr/bin/ffmpeg
js runtime /…/bin/deno
config     /home/…/.config/yt-dlp/config
container  mp4
outdir     /home/…/Downloads
```

It exits `1` when yt-dlp or ffmpeg is missing.

- **YouTube rows fail with "Requested format is not available".** No JavaScript
  runtime. Install deno/node, or install this package with its default
  dependencies.
- **"ffmpeg is not installed" on merge.** Install ffmpeg and reopen the
  terminal; on Windows `winget install Gyan.FFmpeg` then a new window.
- **Cookies fail on Linux** (`cannot decrypt v11 cookies`). The cookie backend
  needs `secretstorage`; the package installs it on Linux automatically. A
  hand-rolled `pip install yt-dlp` without extras does not.
- **Text looks wrong on Windows.** Use Windows Terminal; the legacy console
  host works but renders the box drawing poorly.

## Implementation notes

- **Textual `App.log` collision.** `App` already owns `.log` (a `Logger`);
  defining `def log(self, …)` silently breaks `self.call_from_thread`. The
  helper here is `write_log`.
- **uv shadows the system interpreter.** `yt-dlp`'s launcher resolves its
  interpreter via `/usr/bin/env python3`. Inside a `uv run` script, PATH points
  at uv's managed CPython, which has no `secretstorage`, so
  `--cookies-from-browser …+gnomekeyring` dies with "cannot decrypt v11
  cookies". `child_env()` prunes the uv entries from the child PATH, and adds
  our own `bin`/`Scripts` directory only when the bundled module runs, so the
  bundled deno is visible.
- **A non-zero exit can still carry usable JSON.** yt-dlp exits 1 when a cookie
  backend warns while still emitting complete metadata, so the payload wins
  over the exit code — but a literal `null` payload is an error, not a video.
- **The queue file is written, not flushed per tick.** Every progress update
  marks the queue dirty and a 2 s timer writes it; `on_unmount` flushes one last
  time before `kill_all()`, which is why a paused download still comes back as
  `descargando` on disk and gets re-queued on the next start.
- **One asyncio worker per concurrency slot.** `DownloadManager` reads yt-dlp's
  `--newline` progress from stdout, so no threads and no polling.
- **Symlink launcher.** `sys.path[0]` becomes `~/.local/bin` when `ytd.py` is a
  symlink, so the entry inserts its real directory before importing `app`.

## Tests

```bash
uv run --with textual python test_ytd.py     # no pytest needed
```

Covers codec mapping, format classification, size fallbacks, every filter,
the availability report, progress parsing, the child environment (POSIX and
Windows path syntax), config discovery per OS, yt-dlp resolution order, the
`--check` report, the queue model, the download pool (progress, errors, cancel,
concurrency cap, dedupe), the JSON guard, and the TUI itself (rows, filtering,
selection, status cells, key bindings, and an 80x24 layout regression), plus
queue persistence (round trip, formats and choices, `descargando` → re-queue,
corrupt file, atomic write, restore and auto-resume on start).
