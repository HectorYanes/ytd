# ytd

A queue TUI for [yt-dlp](https://github.com/yt-dlp/yt-dlp): paste URLs, pick the
exact video and audio streams, and watch several downloads at once.

It is a single Go binary. On first use it downloads its own `yt-dlp` and `deno`
into a cache directory, so a fresh machine needs nothing but `ffmpeg`.

```
┌ ytd — yt-dlp queue                                    out ~/Downloads
│ › paste a URL (or several, space separated) and press Enter
│ #  título                  calidad       audio        estado        tamaño
│ 1  Big Buck Bunny 60fps    1080p60 · av1 opus 160k    descargando (42%)  4.1M
│
│ 2160p60 · av1 · video-only
│ 1080p60 · av1 · video-only
│ 1080p   · h264 · video-only
│ audio   · opus · audio-only
│
│ / filter…                              kind:all ext:mp4 sized:false hls:true
│ sel id    kind   res      vcodec acodec hdr fps br     size    ext  src note
│ V   399   video  1080p60  av1    —      —   60  3.0M   —       mp4  —   —
│ A   251   audio  audio    —      opus   —   —   160k   —       webm —   —
│
│ Big Buck Bunny · calidad: 1080p60 · av1 · audio: opus 160k · -f 399+251
```

## Install

Requirements: `ffmpeg` on `PATH`. Everything else is handled by the binary.

- **Prebuilt binary** — grab the archive for your OS/arch from the releases page,
  unpack it, and put `ytd` somewhere on `PATH`.
- **From source** — needs Go 1.26+:

  ```sh
  git clone <repo> ~/tools/ytd && cd ~/tools/ytd
  ./install.sh              # Linux/macOS → ~/.local/bin
  .\install.ps1             # Windows → %LOCALAPPDATA%\ytd\bin
  ```

  Both scripts build with `-trimpath -ldflags "-s -w"`, run `ytd --update` and
  then `ytd --check`.

## First run

```sh
ytd                          # empty queue
ytd URL [URL ...]            # queue these on start
ytd -o ~/Videos -j 2 URL     # other output dir, two parallel downloads
ytd --check                  # environment report, no TUI
ytd --update                 # refresh the managed yt-dlp and deno
```

`--check` prints the version, the resolved yt-dlp (with its version), ffmpeg,
the JavaScript runtime, the yt-dlp config it found, the merge container and the
output directory. It exits non-zero when yt-dlp or ffmpeg is missing.

## Keys

| key | action |
| --- | --- |
| `Enter` (URL bar) | queue every URL in the bar |
| `↓` / `Tab` | leave the URL bar, enter the queue |
| `↑` `↓` `k` `j` | move through the queue |
| `e` | analyze the row; on an analyzed row, edit its streams |
| `v` / `a` | take the explorer's current row as video / audio-only |
| `d` | download the focused row |
| `s` | download every loaded, failed or cancelled row |
| `x` | cancel the focused download |
| `delete` | remove the row (cancelling it if needed) |
| `c` | show only streams compatible with the merge container |
| `r` | reset the choice to the recommended streams |
| `f` | filter the explorer (`1080`, `av1`, `opus`, `muxed`…) |
| `i` / `o` / `h` / `p` | cycle kind, cycle ext, toggle HLS, only sized |
| `l` | show or hide the log |
| `q` | quit (`Ctrl+C` works everywhere) |

In the editor: `Tab` switches video/audio, `↑↓` move, `i` reveals incompatible
streams, `Enter` applies, `Esc` cancels.

## How it works

- **Analysis is lazy and flat.** A URL is analyzed with `yt-dlp -J
  --flat-playlist`; a playlist becomes one row per entry and each row is
  analyzed only when you ask for it.
- **Your yt-dlp config wins.** `ytd` passes no `-f` unless you chose streams, so
  `~/.config/yt-dlp/config` decides; `-P home:<outdir>` still overrides
  `--paths`. The merge container (`--merge-output-format`) is read from that
  config and drives the compatibility matrix: `mp4` accepts h264/h265/av1 with
  aac/mp3/ac3/eac3/alac, `webm` takes vp8/vp9/av1 with opus/vorbis, and `mkv`
  takes anything.
- **Streams are fetched separately.** The first run downloads yt-dlp and deno
  into `<cache>/ytd/bin` (`~/.cache/ytd/bin` on Linux, `~/Library/Caches/...`
  on macOS, `%LOCALAPPDATA%` on Windows) and verifies the published checksums.
  `YTD_YTDLP=/path/to/yt-dlp` and a `yt-dlp` already on `PATH` take precedence.

## Troubleshooting

- **No formats listed for a YouTube URL** — YouTube needs a JavaScript runtime
  to expose formats at all. The app warns at startup; `ytd --update` installs
  `deno`. Without it, even `-f 18` fails with `Requested format is not available`.
- **`ERROR: failed to read from keyring`** — yt-dlp's cookie backend could not
  reach the secret store. Downloads usually still work; unlock the keyring or
  drop `--cookies-from-browser` from the config if it keeps failing.
- **A merge falls back to re-encoding** — one of the two streams is not
  compatible with `--merge-output-format`. The explorer marks those rows and the
  editor flags them; `c` shows only compatible ones.
- **`--check` says yt-dlp is missing** — run `ytd --update`, or install yt-dlp
  yourself and set `YTD_YTDLP`.

## Development

```sh
go test ./...            # unit and program tests, no network
YTD_E2E=1 go test ./...  # adds the real YouTube probes
go vet ./... && gofmt -l .
```

Layout: `internal/format` (the domain model: formats, filters, compatibility,
selection, job), `internal/ytdlp` (the yt-dlp boundary: resolution, config,
progress, verified downloads, analysis, download pool), `internal/ui`
(bubbletea model, view and editor), `cmd/ytd` (flags).

## License

MIT — see `LICENSE`.
