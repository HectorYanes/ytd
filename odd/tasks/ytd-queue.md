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
| T1 | S8 | inline | done | see log |
| T2 | S1, S4, S8 | inline | done | see log |
| T3 | S6, S9 (dedupe), S2/S3 lazy | inline | done | see log |
| T4 | S7 | inline | done | see log |
| T5 | S2/S3 editor, S5 | inline | done | see log |

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
