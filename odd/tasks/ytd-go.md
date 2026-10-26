# ytd — migración total a Go

Objective: replace the Python/Textual implementation with a single Go binary
(bubbletea TUI) that keeps full parity, and fetch yt-dlp/deno itself so a new
machine needs no Python toolchain.
Branch: `go-migration`. Delivery: `single-pr`. Test runner: `go test ./...`

## Specs

- **S1**. Migración total a Go, sin Python en la cadena de ejecución. El pedido
  del usuario, literal: "Hagamos una migracion total de la tool a go".
- **S2**. Paridad funcional con la versión Python: cola con dedupe, análisis
  lazy (`-J --flat-playlist`), expansión de playlists, selección video+audio,
  editor filtrado por contenedor de merge, matriz de compatibilidad, descarga
  en paralelo (default 3), progreso, cancelar, error por fila, log y `--check`.
- **S3**. yt-dlp y deno se consiguen solos: el binario los descarga a un
  directorio gestionado (`<cache>/ytd/bin`) verificando el checksum publicado
  en la release. Decisión del usuario, literal: "Autodescarga gestionada".
  Prioridad: `YTD_YTDLP` → `yt-dlp` del PATH → copia gestionada.
- **S4**. Sin regresiones verificables: los tests portados mantienen las
  expectativas de la suite Python, y un probe real contra YouTube demuestra
  análisis y selección por defecto.

Assumption (flagged): ffmpeg sigue siendo un requisito del sistema. No se
empaqueta: se detecta, se reporta en `--check` y la UI avisa al arrancar.

## Tasks

| id | specs | route | status | commit |
| --- | --- | --- | --- | --- |
| T1 | S2, S4 | inline | done — `internal/format` (modelo, filtros, compatibilidad, informe, selección, job) + 23 tests | f97073d |
| T2 | S3, S4 | inline | done — `internal/ytdlp` (resolución, config por OS, progreso, descarga verificada, análisis, pool) + tests + probe real | this slice |
| T3 | S2 | inline | pending — TUI bubbletea: input, tabla de cola, estados, teclas, log | — |
| T4 | S2 | inline | pending — explorador de formatos, editor modal, filtro `c`, panel de disponibilidad | — |
| T5 | S1, S2 | inline | pending — `cmd/ytd`: flags (`-o`, `-j`, `--check`, `--update`), cableado del app | — |
| T6 | S1, S3 | inline | pending — borrar el árbol Python, installers y README para el binario único | — |

## Log

- **L1** (original request, verbatim): "Hagamos una migracion total de la tool a
  go"
- **L2** (decision, verbatim answer): "Autodescarga gestionada (Recomendada)" —
  el binario baja y actualiza yt-dlp + deno a `~/.cache/ytd/bin`, con URLs fijas
  y verificación de checksums.
- **L3** (decision): el repositorio pasa a `module ytd` con paquetes internos
  (`internal/format`, `internal/ytdlp`, `internal/ui`, `cmd/ytd`). Se conserva
  el árbol Python hasta T6, para que la paridad sea comprobable lado a lado.
- **L4** (evidence): 23 tests de `internal/format` verifican las mismas
  expectativas que la suite Python (mapeo de codecs, clasificación, tamaños,
  filtros con orden, informe de disponibilidad, matriz de compatibilidad,
  selección por defecto 401+140, etiquetas del job).
- **L5** (bug found by the real probe): fusionar stdout y stderr rompía `-J`:
  con `--cookies-from-browser`, yt-dlp escribe "ERROR: failed to read from
  keyring" en stderr, y el pipe compartido insertaba ese texto dentro del JSON.
  `Analyze` ahora captura los dos streams por separado, como hacía Python.
  Lección: el pool de descargas sí quiere streams fusionados; el análisis no.
- **L6** (evidence): probe real (`YTD_E2E=1`) contra Big Buck Bunny: 53 formatos,
  selección por defecto `315+140-drc`, con el yt-dlp del sistema (2026.09.16).
  La diferencia con los 48 formatos de la corrida Python viene de la versión de
  yt-dlp, no de la lógica: el port de `DefaultSelection` está cubierto por test.
