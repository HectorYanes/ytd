# ytd — job queue and parallel downloads

Objective: turn the single-video format explorer into a queue where each pasted
link becomes a row with editable `calidad`, `audio` and live `estado`, downloaded
N-at-a-time.
Branch: `feat/job-queue`. Delivery: `single-pr` (one PR-sized branch, 5 slices).
Test runner: `uv run --with textual python test_ytd.py`

## Specs

- **S1**. Pasting a link appends a row to a queue list. Each row exposes, at
  minimum, the fields `calidad`, `audio` and `estado`, per the user's wording:
  "se agrega a una lista con varios campos a rellenar a lo largo de la tupla".
- **S2**. `calidad` is populated from the video formats actually available for
  that URL: "la calidad, saldra para autocompletar las calidades disponibles".
- **S3**. `audio` works the same way: "los audios q funcionaran de la misma
  manera".
- **S4**. `estado` takes exactly these labels: `cargada`,
  `descargando({Porcentaje de descarga}%)`, `completado`. Failures and
  cancellations must be visible too (`error: …`, `cancelado`).
- **S5**. The app consults video and audio formats that are "disponibles y
  compatibles": incompatible codec/container pairs are flagged, and a filter can
  hide them. Merge container comes from `--merge-output-format` in the user's
  yt-dlp config (currently `mp4`).
- **S6**. Playlist URLs are supported: `yt-dlp -J --flat-playlist` entries each
  become a queue row, without fetching per-video formats up front.
- **S7**. Downloads run in parallel, bounded by a concurrency limit (`-j`,
  default 3).
- **S8**. Existing behaviour preserved: availability summary per video, live
  filtering of the format table, selection of the video+audio pair, `-P home:`
  output-dir override, config-driven cookies/merge/embedding, progress parsing,
  and the 80x24 layout floor.
- **S9**. Per-row failure isolation: an unavailable/private entry or a failed
  download marks only that row, and duplicate URLs are skipped with a notice.

Assumption (flagged): UI strings stay English except the three status labels and
the field names the user named in Spanish; the user was offered a full-Spanish UI
and has not asked for it.

## Tasks

| id | specs | route | status | commit |
| --- | --- | --- | --- | --- |
| T1 | S8 | inline | done | `c9d65ad` |
| T2 | S1, S4, S8 | inline | done | this slice |
| T3 | S6, S9 (dedupe), S2/S3 lazy | inline | partial — dedupe, lazy analyze and per-row isolation done; playlist expansion pending | — |
| T4 | S7 | inline | done — pool + `-j` flag + concurrency test | this slice |
| T5 | S2/S3 editor, S5 | inline | pending | — |

## Log

- **L1** (original request, verbatim): "Quiero una tui para yt-dlp de tal manera
  q pueda ver las calidades y formatos en los que pueda descargar los videos y
  ver si estan disponibles y filtrar entre ellos, investiga si existe sino,
  creala tu"
- **L2** (verbatim): "Si, me sirve, piensalo de esta manera, se consulta los
  formatos de video y audio disponibles y compatibles, al pegar un enlace este
  se agrega a una lista con varios campos a rellenar a lo largo de la tupla, la
  calidad, saldra para autocompletar las calidades disponibles, los audios q
  funcionaran de la misma manera, y la otra el status, como cargada,
  descargando({Porcentaje de descarga}%) y completado"
- **L3** (verbatim): "Soporta playlist y descargas en paralelo ??"
- **L4** (verbatim): "dale, arranca entonces"
- **Decision**: the user accepted the recommended editor model (queue table +
  format explorer for the focused row), not per-row `Select` widgets.
- **Decision**: the default download path passes no `-f` at all, so yt-dlp uses
  the config's own `--format "bv*+ba/b"`; per-video formats are fetched lazily
  only when the row is inspected.
- **L5** (implementation, T2+T4): `core.py` grew `YtdlpError`, `decode_json`,
  `run_json`, `download_command` and `DownloadManager` (N asyncio workers,
  `--newline` progress, cancel, dedupe, per-row error isolation). `app.py` is
  the queue UI (DataTable + format explorer for the focused row, `JobChanged`
  message for per-row redraw, lazy `e` analyze). `ytd.py` is now a thin entry
  with the `sys.path` fix and the `-j/--j` flag (default 3). Tests: 39/39.
  Evidence: real end-to-end on Big Buck Bunny — 47 formats parsed, default
  pick `315+258`, explicit `-f 18` downloaded to the captured path with
  progress samples 0→100%, `completado`. `run_json` found a real bug: an
  unavailable video makes yt-dlp print the literal `null` with exit 1, which
  is now a `YtdlpError` instead of a `NoneType` crash.
- **Decision** (T4): the confirm modal for >20 rows was dropped; it was my
  idea, not a spec, and `s` is already explicit.
