# ytd — a format explorer TUI for yt-dlp

`yt-dlp -F` prints an unreadable wall of 47 rows. You cannot search it, you
cannot see which qualities need a separate audio stream, and you cannot tell a
progressive MP4 from an HLS variant without squinting. `ytd` turns that output
into a filterable table and downloads exactly what you pick.

## Install

```bash
# already installed at ~/.local/bin/ytd (symlink into ~/.local/bin)
ln -sf ~/tools/ytd/ytd.py ~/.local/bin/ytd

ytd                      # empty, paste a URL
ytd 'https://youtu.be/…' # analyze on start
ytd -o ~/Videos 'URL'    # custom output directory
```

Requires `yt-dlp` and `ffmpeg` in `PATH`. The script itself is a PEP 723 uv
script: `uv run` provisions Textual on first launch, nothing global.

## Keys

| key | action |
| --- | --- |
| `Enter` in the URL field | analyze the URL |
| `v` | set the highlighted row as the **video** part |
| `a` | set the highlighted row as the **audio** part |
| `d` | download the current selection |
| `y` | copy the equivalent `yt-dlp -f …` command |
| `f` | jump to the filter box |
| `l` | toggle the log pane |
| `r` | clear the selection |
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

## Interaction with your yt-dlp config

`~/.config/yt-dlp/config` is still loaded (cookies, output template, merge
container, embedding). `ytd` only overrides two things:

- `-f` — your selection wins over `--format` in the config.
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
  backend warns while still emitting complete metadata, so the JSON payload is
  preferred over the exit code.

## Tests

```bash
uv run --with textual python test_ytd.py     # 23 tests, no pytest needed
```

Covers codec mapping, format classification, size fallbacks, every filter,
the availability report, progress parsing, the child environment, and the
TUI itself (row rendering, filtering, selection, command building, and a
small-terminal layout regression at 80x24).

## Not implemented yet

- Playlists (the app says so instead of guessing which entry you meant).
- Live manifests beyond what the `formats` array exposes.
- Per-format subtitle and chapter pickers — the config's defaults apply.
