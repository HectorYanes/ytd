#Requires -Version 5.1
<#
.SYNOPSIS
    Install ytd on Windows: uv, ffmpeg, then the tool itself.

.DESCRIPTION
    Idempotent. Installs uv and ffmpeg with winget when they are missing,
    installs the wheel next to this script (or the source tree when there is
    no wheel) as a uv tool, then runs `ytd --check` to confirm the result.
    ASCII only, so Windows PowerShell 5.1 reads it in any code page.

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\install.ps1
#>
[CmdletBinding()]
param(
    [switch]$SkipCheck,
    [switch]$SkipFfmpeg
)

$ErrorActionPreference = 'Stop'

function Write-Step([string]$text) { Write-Host "-- $text" }
function Write-Fail([string]$text) { Write-Host "error: $text" -ForegroundColor Red; exit 1 }

# Re-read PATH from the registry plus the uv tool directory, so tools installed
# by this script are usable without opening a new window.
function Update-SessionPath {
    $machine = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    $user = [Environment]::GetEnvironmentVariable('Path', 'User')
    $parts = @($machine, $user, $env:Path) | Where-Object { $_ }
    $env:Path = ($parts -join ';')
    $toolBin = Join-Path $env:USERPROFILE '.local\bin'
    if ($env:Path -notlike "*$toolBin*") {
        $env:Path = "$toolBin;$env:Path"
    }
}

function Install-WingetPackage([string]$id) {
    if (-not (Get-Command winget -ErrorAction SilentlyContinue)) { return $false }
    & winget install --id $id -e --accept-source-agreements --accept-package-agreements | Out-Host
    Update-SessionPath
    return ($LASTEXITCODE -eq 0)
}

if ($PSScriptRoot) {
    $root = $PSScriptRoot
} else {
    $root = (Get-Location).Path
}

Write-Host '== ytd installer (Windows)'

# ---- 1. uv -----------------------------------------------------------------
if (-not (Get-Command uv -ErrorAction SilentlyContinue)) {
    Write-Step 'uv not found, installing it'
    if (-not (Install-WingetPackage 'astral-sh.uv')) {
        Write-Step 'winget did not have it, using the official installer'
        Invoke-RestMethod https://astral.sh/uv/install.ps1 | Invoke-Expression
        Update-SessionPath
    }
    if (-not (Get-Command uv -ErrorAction SilentlyContinue)) {
        Write-Fail 'uv was installed but is not on PATH yet; open a new terminal and rerun'
    }
}
Write-Step "uv $((& uv --version) -replace '^uv ', '')"

# ---- 2. ffmpeg -------------------------------------------------------------
if (-not $SkipFfmpeg) {
    if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
        Write-Step 'ffmpeg not found, installing it'
        if (-not (Install-WingetPackage 'Gyan.FFmpeg')) {
            Write-Warning 'ffmpeg is missing: merging video+audio and embedding will fail until you install it (winget install Gyan.FFmpeg)'
        }
    }
    if (Get-Command ffmpeg -ErrorAction SilentlyContinue) {
        $version = (& ffmpeg -version 2>$null | Select-Object -First 1) -split ' ' | Select-Object -Index 2
        Write-Step "ffmpeg $version"
    }
}

# ---- 3. what to install ----------------------------------------------------
# Prefer the newest built wheel next to this script; fall back to the source.
$wheel = $null
$distPath = Join-Path $root 'dist'
if (Test-Path $distPath) {
    $wheel = Get-ChildItem $distPath -Filter 'ytd_tui-*.whl' -File |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 1
}
if ($wheel) {
    $target = $wheel.FullName
} else {
    $target = $root
}
Write-Step "package: $target"

$bin = "$(& uv tool dir --bin)".Trim()
$toolList = & uv tool list 2>$null
$managed = $toolList -match '^ytd-tui\s'
$entry = Join-Path $bin 'ytd.exe'

# A foreign ytd in the tool bin directory would be replaced without notice.
if ((Test-Path $entry) -and -not $managed) {
    if ($env:YTD_ALLOW_OVERWRITE -eq '1') {
        Write-Step "replacing the existing $entry"
    } else {
        Write-Fail "$entry already exists and is not managed by uv. Remove it and rerun, or set `$env:YTD_ALLOW_OVERWRITE=1."
    }
}

# ---- 4. install ------------------------------------------------------------
Write-Step 'installing the tool'
& uv tool install --force $target | Out-Host
if ($LASTEXITCODE -ne 0) { Write-Fail 'uv tool install failed' }

if (-not $env:UV_TOOL_BIN_DIR) {
    & uv tool update-shell 2>$null | Out-Null
}
Update-SessionPath

# ---- 5. verify -------------------------------------------------------------
if (-not (Test-Path $entry)) { Write-Fail "install finished but $entry is missing" }
$version = & $entry --version 2>$null
if ($version -notlike 'ytd *') { Write-Fail "the installed entry point did not run: $entry --version" }

Write-Host ''
& $entry --version
if ($SkipCheck) {
    Write-Host ''
    Write-Host "ready: run 'ytd' to open the queue."
    exit 0
}

& $entry --check
$checkCode = $LASTEXITCODE
Write-Host ''
if ($checkCode -eq 0) {
    Write-Host "ready: run 'ytd' to open the queue."
} else {
    Write-Host 'installed, but --check reported missing pieces above; fix those and'
    Write-Host "run 'ytd --check' again before using it."
}
exit 0
