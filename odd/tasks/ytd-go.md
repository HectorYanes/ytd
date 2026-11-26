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

- **S5**. Rediseño completo de la TUI: más holgado, cómodo y parecido a Python.
  El explorador de formatos es la superficie principal y se navega con flechas,
  PageUp/PageDown y mouse (rueda y clic), sin popup obligatorio. El pedido del
  usuario, literal: "En la tui no hay una manera comoda de desplazarze, has un
  rediseño completo, mas olgado, mas comodo, mas como el de python". Decisión
  del fork, literal: "A" (explorador estilo Python, sin popup forzado).
- **S6**. Segunda ronda de layout y estado, pedido del usuario, literal: "has
  solo push, luego de eso quiero q el log en ves de una barra en la parte de
  abajo sea vertical la barra de log, y q la barra de progreso del video se
  muestre en la misma cola, ademas, en python esto tenia persistencia, aca no
  hay nada, otra cosa, ponle bordes a los containers para ver bien las
  divisiones".
  - **S6.1**. El log es un panel vertical a la derecha del cuerpo (borde
    propio), no una tira al pie. El fallback a bloque inferior que tenía esta
    spec queda derogado por S7.3: en terminales chicas el log se esconde.
  - **S6.2**. La fila de la cola muestra su propia barra de progreso en la
    columna `estado` mientras descarga (`███░░ 42%`), y la línea meta del video
    enfocado suma `%`, velocidad y ETA. No hay línea de progreso separada.
  - **S6.3**. Persistencia de la cola con paridad Python (`ytd_tui/store.py`):
    un JSON por usuario (`XDG_STATE_HOME`/`LOCALAPPDATA`/`Application Support`
    → `ytd/queue.json`), escritura atómica y defensiva, y las filas que estaban
    descargando vuelven como `cargada` y se reencolan al arrancar.
  - **S6.4**. Todos los paneles (URL, cola, formatos, log) llevan borde redondeado
    con el título en el borde superior; el borde se resalta cuando el panel tiene
    el foco.
- **S7**. Tercera ronda de UI, pedido del usuario (resumen del mensaje, el texto
  original no se conservó verbatim en esta sesión): partir el panel de formatos
  en dos, elegir solo con Enter, log abierto por defecto y tablas más angostas.
  - **S7.1**. El explorador son dos cajas apiladas. Arriba, la tabla de videos;
    abajo, la de formatos progresivos (muxed), que ocupa la mitad del espacio
    anterior y solo aparece cuando la fila tiene formatos muxed.
  - **S7.2**. La selección es solo con Enter. Enter sobre un video pasa la tabla
    de arriba a los audios que se pueden mergear con ese video; Enter sobre un
    audio lo fija; Esc vuelve a la lista de videos y un segundo Esc sale a la
    cola. Las teclas `v` y `a` ya no eligen nada.
  - **S7.3**. El log arranca abierto; se esconde solo cuando la terminal no
    alcanza (menos de 76 columnas o menos de 24 filas) y `l` avisa en vez de
    abrirlo en un espacio que no entra. No hay fallback de log inferior.
  - **S7.4**. La tabla de videos no muestra `id`, `ext`, `acodec` ni `hdr`, y
    ofrece únicamente los videos con la extensión del contenedor de merge
    (`mp4`→mp4, `webm`→webm, `mkv`/desconocido sin filtro; si el filtro vacía la
    lista se muestran todos). La tabla muxed no muestra `id` ni `ext`.
  - **S7.5**. La tabla muxed es navegable y focuseable (Tab, flechas, clic,
    Enter): Enter sobre un muxed fija `VideoID` y limpia `AudioID`.
- **S8**. El pedido del usuario, literal: "La descarga se mantiene todo el
  tiempo en 0%". La descarga tiene que poder explicarse sola y mostrar
  movimiento desde el arranque.
  - **S8.1**. El log muestra las líneas de yt-dlp que no son progreso (cookies,
    extracción, reintentos, miniatura, merge, `ERROR:`) con el número de fila
    delante (`#1 …`), como máximo 40 por descarga, sin repetir seguidas. Una
    transferencia detenida deja de ser un 0% mudo.
  - **S8.2**. Por debajo de 10% la celda `estado` muestra un decimal (`0.4%`);
    de 10% en adelante, entero (`42%`). El ancho de la celda no cambia.
  - **S8.3**. Cuando hay merge, la ruta de la fila es el archivo final, no el
    `.fNNN` intermedio que yt-dlp borra.

## Tasks

| id | specs | route | status | commit |
| --- | --- | --- | --- | --- |
| T1 | S2, S4 | inline | done — `internal/format` (modelo, filtros, compatibilidad, informe, selección, job) + 23 tests | f97073d |
| T2 | S3, S4 | inline | done — `internal/ytdlp` (resolución, config por OS, progreso, descarga verificada, análisis, pool) + tests + probe real | this slice |
| T3 | S2 | inline | done — `internal/ui`: barra de URL, cola, informe, filtros, explorador, slots, log | 4b1f2ff |
| T4 | S2 | inline | done — editor modal (video/audio, tab, `i`), `c` compat, panel de disponibilidad | 4b1f2ff |
| T5 | S1, S2 | inline | done — `cmd/ytd`: `-o`, `-j`, `--check`, `--update`, `--version`, URLs | 4b1f2ff |
| T6 | S1, S3 | inline | done — árbol Python borrado, installers y README para el binario único | este slice |
| T7 | S5 | inline | done — `internal/ui/view.go` rediseñado (barra tipo Textual, explorador de formatos dominante, colores por kind, slots y progreso) + navegación URL→cola→formatos→filtro, rueda/clic de mouse, 7 tests nuevos | 454d74c |
| T8 | S6.3 | inline | done — `internal/store` (state dir por OS, JSON atómico, dedupe/UIDs, resume de descargas interrumpidas) + cableado en `ui.New`/`Init` con flush cada 2 s + 8 tests | este slice |
| T9 | S6.1, S6.2, S6.4 | inline | done — caja con borde y título para URL/cola/formatos/log, log vertical de ancho completo del cuerpo, progreso en la celda `estado` y %/velocidad/ETA en la meta + 4 tests | este slice |
| T10 | S7.1–S7.5 | inline | done — explorador partido en videos/muxed, selección Enter-only con `stage`/`stageUID`, audios filtrados por compatibilidad, `format.BestAudio`, columnas por tabla y log abierto con auto-hide + 11 tests nuevos | este slice |
| T11 | S8.1–S8.3 | inline | done — `Manager.OnLine` reenvía el chatter de yt-dlp al log (tope 40, sin repetidas), décimas por debajo de 10% y `Path` del archivo mergeado; 7 tests nuevos, test de pantalla sin carrera tras `8ede055` | `8af0413` + `8ede055` |

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

- **L7** (evidence): 68 tests en `go test ./...`, más dos probes opt-in con
  `YTD_E2E=1`: `TestAnalyzeRealURL` (53 formatos) y
  `TestProgramRunsAgainstYouTube`, que corre el programa real con
  `tea.Program` headless, espera el análisis, manda `q` y verifica título,
  selección y que algo se renderizó.
- **L8** (decision): en la barra de URL, `q` con el campo vacío sale; con texto
  escrito se escribe la `q`. Sin eso el primer estado de la app no tenía salida
  por teclado salvo `Ctrl+C` (Textual tenía la misma trampa y no la quisimos
  copiar).
- **L9** (evidence): cross-compila limpio para linux/amd64, linux/arm64,
  darwin/amd64, darwin/arm64 y windows/amd64 con `-s -w`: 6.9–7.7 MB por
  binario, contra los 148–152 MB que pesaba la instalación Python con deno y
  site-packages.
- **L10** (pending): `install.ps1` sigue sin probarse en Windows real (no hay
  PowerShell en esta máquina); el binario windows/amd64 sí compila.
- **L11** (diagnosis): el error `The page needs to be reloaded` del video
  `jTWb-RIdN-I` era ambiental, no del código: el yt-dlp del sistema
  (2026.09.16) fallaba el challenge JS con deno. Con yt-dlp 2026.11.24 el mismo
  URL devuelve 32 formatos y el análisis Go funciona sin cambios.
- **L12** (user request, verbatim): "En la tui no hay una manera comoda de
  desplazarze, has un rediseño completo, mas olgado, mas comodo, mas como el de
  python".
- **L13** (decision, verbatim answer): "A" — explorador estilo Python: la tabla
  de formatos es la superficie, sin modal obligatorio; `v`/`a` eligen, sin
  elección va `best`. Implementado: paneles URL→cola→formatos→filtro con `tab`,
  flechas y PageUp/Down en formatos, rueda y clic de mouse, y `enter` en la
  barra de URL lleva directo al explorador.
- **L14** (evidence): commit `454d74c` (`feat(ui): redesign the TUI...`).
  `go test -count=1 ./...` verde; `gentle-ai review assess --base-ref 0e489d1
  --committed-only` devuelve `risk: medium`, `review_due: false`
  (`under_budget`). El probe real `YTD_E2E=1 TestProgramRunsAgainstYouTube`
  vuelve a pasar con el layout nuevo (59 s, análisis real y `q`).
- **L15** (decision): la persistencia no se inventa: se porta `ytd_tui/store.py`
  de la rama Python (`feat/job-queue`) con el mismo esquema JSON (`version: 1`,
  `jobs[]` con `uid`, `url`, `title`, `uploader`, `duration`, `formats`,
  `video_id`, `audio_id`, `status`, `percent`, `speed`, `eta`, `size`, `error`,
  `path`). Las lecturas son defensivas: archivo corrupto o ajeno ⇒ cola vacía y
  un aviso en el log, nunca un aborto. Los `Downloading` vuelven como `cargada`
  y se reencolan.
- **L16** (evidence): `go test -count=1 ./...` verde (14 tests nuevos: 8 de
  `internal/store`, 6 de UI entre progreso, bordes/log vertical, persistencia,
  resume, archivo corrupto y flush al salir), `go vet` limpio, `gofmt` limpio y
  `YTD_E2E=1 TestProgramRunsAgainstYouTube` pasa en 7.2 s con el layout de
  bordes y el log vertical.
- **L17** (user request, resumen): partir el panel de formatos en dos (videos
  arriba, muxed abajo solo si existen), selección solo con Enter (sin `v`/`a`),
  log abierto por defecto con auto-hide en terminales chicas y tablas con menos
  columnas. El texto original no se conservó verbatim en esta sesión.
- **L18** (decision): el split se modela con `stage` (`stageVideo`/`stageAudio`)
  y `stageUID`, así cambiar de fila vuelve solo a la lista de videos. Elegir un
  video conserva el audio si sigue siendo compatible y si no toma el mejor con
  `format.BestAudio`. Los audios se filtran por `format.IsCompatible` y los
  videos por la familia del contenedor de merge; los dos filtros tienen fallback
  a la lista completa para no dejar un callejón sin salida. Se eliminaron el
  filtro `kind` (`i`) y su columna porque el split ya separa por tipo.
- **L19** (evidence): `go test -count=1 ./...` verde, `go vet` y `gofmt` limpios;
  `YTD_E2E=1 TestProgramRunsAgainstYouTube` pasa en 11.2 s. Se revisó el render
  real a 120x40, 96x30 y 70x22 en las dos etapas: a 70x22 el log se esconde solo
  y quedan las dos cajas apiladas.
- **L20** (evidence): `gentle-ai review assess --base-ref 0afa695
  --committed-only` sobre este slice devuelve `risk: medium`,
  `review_due: false` (`under_budget`); RDD sigue activo y el boundary queda en
  este commit.
- **L21** (user request, verbatim): "La descarga se mantiene todo el tiempo en
  0%"
- **L22** (diagnosis): no se pudo reproducir un porcentaje congelado. Con el
  binario real en un pty, contra el `queue.json` real del usuario (restaurado,
  con las cookies de brave y el mismo video), la descarga completó al 100% y el
  manager emitió snapshots 0.1→100; un probe headless con la ruta completa de
  bubbletea (`Init`→`waitUpdate`→`applySnapshot`→`View`) mostró la barra
  subiendo 0→100 en pantalla. El archivo de cola del usuario (02:33) tenía
  `percent 0`, `speed ""`, `size ""` y `path ""`: yt-dlp no había emitido ni
  `Destination:` antes de que la sesión terminara. Un probe propio reprodujo
  `[download] Got error: … Network is unreachable. Retrying (1/10)`, con 20–30 s
  de silencio. Defecto real: `Manager.download` descartaba toda línea que no
  fuera progreso, así que una descarga detenida no dejaba rastro visible; y el
  entero de la celda se queda en `0%` durante más de un minuto en una
  transferencia grande (el default de BBB son 1.27 GiB a ~500 KiB/s).
- **L23** (decision): `Manager.OnLine` manda el chatter al log por un canal
  propio (nunca demora un progreso), con tope de 40 líneas y sin repetidas
  consecutivas; por debajo de 10% la celda muestra décimas para que el
  movimiento se vea; y el `[Merger] Merging formats into` fija la ruta final en
  vez del `.fNNN` que yt-dlp borra.
- **L24** (evidence): `go test -count=1 ./...` verde (6 tests nuevos),
  `go vet`/`gofmt` limpios; render real a 120x40 con `ytd -o … BBB` mostrando
  `0.1% → 0.6%` en la fila y las líneas `#1 [youtube] …`/`#1 [download]
  Destination: …` en el panel.
- **L25** (review finding, accepted): el test de pantalla nuevo leía
  `model.logLines` desde la goroutine del test mientras `program.Run()`
  escribía en ese slice; `go test -race` lo confirmó
  (`writeLog`/`startAll` vs. `logHas`). Corregido en `8ede055`: el test ahora
  alimenta el modelo directo por `waitLog` en la goroutine del test, sin
  `tea.Program`, sin sleeps y sin estado compartido, y además fija el prefijo
  `#<fila>` y el borde 9.94→9.95→10 con ancho de celda constante.
  `go test -race -count=1 ./...` verde, `go vet`/`gofmt` limpios.
- **L26** (decision): la revisión nativa no pudo correr porque los agentes
  `review-*`, `jd-*` y el verificador apuntan a `opencode/glm-5.3-flash-free`,
  modelo que no existe en este OpenCode (el catálogo ofrece
  `opencode/glm-5.3-flash`). El usuario eligió saltear la revisión: se ejecutó
  `gentle-ai review mode disable --scope clone`, que deja RDD apagado solo en
  este clon (global sigue en `on`). Reencenderlo: `gentle-ai review mode
  enable --scope clone`, después de arreglar el nombre del modelo.
