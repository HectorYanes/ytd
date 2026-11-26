# ytd — cross-platform distribution

Objective: make `ytd` installable and usable on Windows/macOS/Linux by someone
else, without depending on this machine's paths, symlinks or system packages.
Branch: `feat/cross-platform`. Delivery: `single-pr`.
Test runner: `uv run --with textual python test_ytd.py`

## Specs

- **S1**. The app must work the same on Windows: queue, analysis, download,
  playlists and parallel downloads, per the user's words: "Ahora quiero poder
  pasarselo a alguien externo y multisitema, osea, q funcione en windows igual".
- **S2**. It must be handable to an external person: documented, reproducible
  install with no absolute paths, no symlink that only exists here, and no
  reliance on system packages that Windows lacks.
- **S3**. Platform differences must be resolved in code, not by hand: yt-dlp
  location, yt-dlp config location, `ffmpeg`, and the JavaScript runtime
  YouTube now requires. A `--check` diagnostic must report all of them.
- **S4**. No regression on Linux: every existing behavior keeps working, the
  full test suite passes, and a real analysis/download still succeeds.

Assumption (flagged): distribution is a Python package (`uv tool install` /
`pipx`) plus a no-install `ytd.cmd`/`ytd.py` path; a single `.exe` was not
requested and would still need ffmpeg.

## Tasks

| id | specs | route | status | commit |
| --- | --- | --- | --- | --- |
| T1 | S2 | inline | done — package `ytd_tui/`, `pyproject.toml`, console script, PEP 723 shim, `ytd.cmd` | this slice |
| T2 | S1, S3 | inline | done — yt-dlp resolution (env/PATH/bundled module), bundled deno, UTF-8 child I/O, cross-platform `child_env` | this slice |
| T3 | S3 | inline | done — config discovery per OS, ffmpeg + JS runtime probes, `ytd --check` | this slice |
| T4 | S2, S4 | inline | done — README per OS, LICENSE, real-run evidence | docs slice |
| T5 | S2 | inline | done — `install.sh` / `install.ps1` (uv + ffmpeg + tool + `--check`), conflict guard, tested on Linux; PowerShell untested locally | installers slice |

## Log

- **L1** (original request, verbatim): "Ahora quiero poder pasarselo a alguien
  externo y multisitema, osea, q funcione en windows igual"
- **Decision**: `yt-dlp[default,deno]` is a hard dependency. Without a JS
  runtime YouTube returns no formats at all ("Requested format is not
  available", even with `-f 18`), so a fresh Windows install would look broken.
  The deno wheel is ~92 MB; that is the price of working out of the box.
- **Decision**: precedence for yt-dlp is `YTD_YTDLP` → `yt-dlp` on PATH →
  bundled module. Linux/macOS users keep their own install and config; the
  bundled copy is what makes Windows (and `uv tool install`) work.
- **Decision**: `ffmpeg` is NOT bundled (~80 MB of third-party wheels for a
  binary the user may already have). It is probed, reported by `--check`, and
  warned about in the UI at startup; muxed downloads still work without it.
- **L2** (evidence): with `PATH=/nonexistent` and a wheel installed into a
  throwaway tool dir, `ytd --check` reports `bundled module` + injected
  `deno`, and `run_json` returns 48 formats. That is the Windows shape: no
  system yt-dlp, no deno on PATH.
- **L3** (evidence): the project venv exposed a real defect — a `yt-dlp`
  installed without the `secretstorage` extra breaks
  `--cookies-from-browser brave+gnomekeyring` on Linux. `yt-dlp[secretstorage]`
  is now a Linux-only dependency, verified with a cookie-backed `-J` run.
- **L4** (evidence): `uv build` → wheel → `uv tool install` into a scratch
  tool dir → `ytd --version` / `ytd --check` / console script all work.
- **L5** (evidence): `uv tool install` of the final wheel, run with
  `PATH=/nonexistent`, reports `bundled module` + bundled `deno` and exits `1`
  only because ffmpeg is absent from that stripped PATH; `run_json` extracted
  48 formats. Full suite 60/60, pyflakes clean, wheel ships `LICENSE` and the
  console script.
- **L6** (scope): `ffmpeg` stays a documented prerequisite — the ~92 MB deno
  wheel is bundled because YouTube is the primary target; doubling it for a
  binary most systems already have is not worth it. Windows real-machine
  testing was not possible; Windows behavior is covered by pure-function tests
  (`_prune_path`, `config_candidates`, `child_env`) plus the platform-agnostic
  bundle path proven above.
- **L7** (installers): `install.sh` tested in three modes: normal (wrapped
  wheel installed, `--check` clean, exit 0), foreign `ytd` in the tool bin dir
  (refuses with exit 1 and the exact `rm` command), and
  `YTD_ALLOW_OVERWRITE=1` (uv replaces the symlink with its own and the entry
  point answers `--version`). `install.ps1` could not be executed here: there
  is no PowerShell on this machine, so it is reviewed but untested — the first
  real Windows run is the pending proof.
- **L8** (size): installed footprint measured at 148 MB — `bin/deno` 92 MB and
  `lib/.../site-packages` 55 MB (yt-dlp 12, cryptography 15, Cryptodome 6.8,
  pygments 5, brotli 5, textual 3.3). The uv wheel cache and the uv-managed
  CPython are shared outside the tool directory.
