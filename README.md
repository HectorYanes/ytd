# ytd — a queue TUI for yt-dlp

Paste one link or twenty. Each one becomes a row with the quality it will
download, the audio that goes with it, and a live status. The format table
below is for the focused row only, so you see 8 filters and one video instead
of `yt-dlp -F`'s wall of 47 rows.

## Install

```bash
# already installed at ~/.local/bin/ytd (symlink into ~/tools/ytd/ytd.py)
ln -sf ~/tools/ytd/ytd.py ~/.local/bin/ytd

ytd                                  # empty queue, paste URLs
ytd 'https://youtu.be/…' 'https://…'  # queue them on start
ytd -o ~/Videos -j 4 'URL'           # custom dir, 4 downloads at once
```

Requires `yt-dlp` and `ffmpeg` in `PATH`. The script is a PEP 723 uv script:
`uv run` provisions Textual on first launch, nothing global.

## The queue

| column | meaning |
| --- | --- |
| `#` | row order |
| `título` | video title (the URL until analyzed) |
| `calidad` | video stream that will be used, `auto (best)` when unnamed |
| `audio` | audio stream, or `— (incluido)` for muxed formats |
| `estado` | `cargada`, `descargando (45%)`, `completado`, `error: …`, `cancelado` |
| `tamaño` | expected size from the format metadata |

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
| `e` | analyze the focused row (formats are fetched lazily) |
| `d` | download the focused row |
| `s` | download every pending row |
| `x` | cancel the focused download |
| `Delete` | remove the focused row |
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

## Interaction with your yt-dlp config

`~/.config/yt-dlp/config` is still loaded (cookies, output template, merge
container, embedding). `ytd` only overrides two things:

- `-f` — your selection wins over `--format` in the config, and only when you
  selected something.
- `-P home:<dir>` — the output directory, verified to override a `--paths`
  entry from the config.

## Implementation notes

- **Textual `App.log` collision.** `App` already owns `.log` (a `Logger`);
  defining `def log(self, …)` silently breaks `self.call_from_thread`. The
  helper here is `write_log`.
- **uv shadows the system interpreter.** `yt-dlp`'s launcher resolves its
  interpreter via `/usr/bin/env python3`. Inside a `uv run` script, PATH points
  at uv's managed CPython, which has no `secretstorage`, so
  `--cookies-from-browser …+gnomekeyring` dies with "cannot decrypt v11
  cookies". `child_env()` prunes the uv entries from the child PATH.
- **A non-zero exit can still carry usable JSON.** yt-dlp exits 1 when a cookie
  backend warns while still emitting complete metadata, so the payload wins
  over the exit code — but a literal `null` payload is an error, not a video.
- **One asyncio worker per concurrency slot.** `DownloadManager` reads yt-dlp's
  `--newline` progress from stdout, so no threads and no polling.
- **Symlink launcher.** `sys.path[0]` becomes `~/.local/bin` when `ytd.py` is a
  symlink, so the entry inserts its real directory before importing `app`.

## Tests

```bash
uv run --with textual python test_ytd.py     # no pytest needed
```

Covers codec mapping, format classification, size fallbacks, every filter,
the availability report, progress parsing, the child environment, the queue
model, the download pool (progress, errors, cancel, concurrency cap, dedupe),
the JSON guard, and the TUI itself (rows, filtering, selection, status cells,
key bindings, and an 80x24 layout regression).
