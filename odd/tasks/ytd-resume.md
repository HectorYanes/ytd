# ytd — la cola sobrevive al reinicio y las descargas se reanudan

Objective: guardar la cola en disco para que cerrar la TUI no pierda el trabajo,
y al volver reencolar automáticamente lo que estaba descargando.
Branch: `feat/job-queue`. Delivery: `single-pr`. Test runner:
`uv run --with textual python test_ytd.py`

## Specs

- **S1**. Al salir de la TUI el proceso muere y las descargas en curso se pausan;
  eso está bien. Lo que no se pierde es la cola: pedido del usuario, literal:
  "al salir y entraar de la tui, el proceso en la ui se muere, no quiero eso,
  quiero poder reanudarlo, al salir de la tui lo q se este descargando esta
  bien q se pause, pero cuando vuelva deberia poder continuar lo q ya se estaba
  descargando".
- **S2**. Al volver, se restaura cada fila completa: URL, título, uploader,
  duración, formatos ya analizados, elección video/audio, estado, error, path y
  porcentaje. Las filas ya analizadas no se vuelven a analizar.
- **S3**. Las filas que estaban en `descargando` vuelven como `cargada` y se
  reencolan solas al arrancar, reanudando el archivo parcial (`--continue`).
- **S4**. La persistencia nunca rompe el arranque: archivo ausente, corrupto, de
  otra versión o ilegible deja la cola vacía y escribe un aviso en el log.
- **S5**. Borrar una fila la saca del archivo en el siguiente guardado; una cola
  vacía deja el archivo con cero filas.

Assumption (flagged): no hay coordinación entre dos instancias simultáneas;
gana la última que guarda. Documentado en el README como limitación.

## Tasks

| id | specs | status | evidence |
| --- | --- | --- | --- |
| T1 | S1, S2, S4, S5 | done — `ytd_tui/store.py`: rutas por OS, lectura/escritura atómica, serialización tolerante | 7 tests de store en verde |
| T2 | S3 | done — `--continue` explícito en `download_command` | `test_download_command_continues_partial_files` |
| T3 | S2, S3 | done — `app.py`: restaurar en `on_mount`, guardar con debounce y en `on_unmount`, reencolar | restore, resume y save-on-exit en verde |
| T4 | S1 | done — README: comportamiento de pausa/reanudación y limitación de instancias | sección "The queue" + nota de implementación |

## Log

- **L1** (original request, verbatim): "al salir y entraar de la tui, el proceso
  en la ui se muere, no quiero eso, quiero poder reanudarlo, al salir de la tui
  lo q se este descargando esta bien q se pause, pero cuando vuelva deberia
  poder continuar lo q ya se estaba descargando"
- **L2** (decision): el estado vive en `$XDG_STATE_HOME/ytd/queue.json`
  (`~/.local/state/ytd/queue.json` por defecto; `%LOCALAPPDATA%` en Windows;
  Application Support en macOS). No en el repo ni en el directorio de descargas.
- **L3** (decision): las filas `descargando` se guardan tal cual y se traducen a
  `cargada` al cargar, así una salida limpia y una caída terminan en el mismo
  camino: reencolar y reanudar el `.part`.
- **L4** (decision): guardado con debounce de 2 s más un flush en `on_unmount`,
  porque cada tick de progreso emite un `JobChanged` y no queremos un archivo por
  tick.
- **L5** (evidence): `uv run --with textual python test_ytd.py` → 71/71. La suite
  pasa a aislar `XDG_STATE_HOME` por test porque el archivo de cola es estado
  real y los tests de la TUI se contaminaban entre sí.
- **L6** (decision): `restore_queue()` se llama después de `manager.start()` y
  antes de sembrar `--seed`/URLs de la línea de comandos, así el contador de
  `uid` arranca por encima de lo restaurado y no hay colisiones.
