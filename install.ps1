# Build ytd from source on Windows and put the binary on the user PATH.
#
#   .\install.ps1
#
# Needs Go 1.26+ only for this build. The installed binary needs nothing but
# ffmpeg; it fetches yt-dlp.exe and deno itself on first use.
$ErrorActionPreference = "Stop"

Set-Location -Path $PSScriptRoot

$prefix = if ($env:PREFIX) { $env:PREFIX } else { Join-Path $env:LOCALAPPDATA "ytd" }
$binDir = Join-Path $prefix "bin"
$version = try { (git describe --tags --always --dirty 2>$null) } catch { "dev" }
if (-not $version) { $version = "dev" }

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error "Go is required to build from source (https://go.dev/dl/). Prebuilt binaries: releases page."
    exit 1
}

Write-Host "ytd: building $version for windows/$env:PROCESSOR_ARCHITECTURE..."
New-Item -ItemType Directory -Force -Path $binDir | Out-Null
go build -trimpath -ldflags "-s -w -X main.version=$version" -o (Join-Path $binDir "ytd.exe") ./cmd/ytd
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
    Write-Warning "ffmpeg is missing: merging separate video+audio streams will fail. winget install Gyan.FFmpeg"
}

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$binDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$binDir", "User")
    Write-Host "ytd: added $binDir to the user PATH (open a new terminal)."
}

Write-Host "ytd: installed $binDir\ytd.exe"
& (Join-Path $binDir "ytd.exe") --update
& (Join-Path $binDir "ytd.exe") --check
