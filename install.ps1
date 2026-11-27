# Build ytd from source on Windows and put the binary on the user PATH.
#
#   .\install.ps1
#
# Needs Go 1.26.5+ only for this build. The installed binary needs nothing but
# ffmpeg; it fetches yt-dlp.exe and deno itself on first use.
$ErrorActionPreference = "Stop"

$repoDir = $PSScriptRoot
$tempDir = $null

try {
    $hasSource = $false
    if ($repoDir) { $hasSource = Test-Path (Join-Path $repoDir "go.mod") }
    if (-not $hasSource) {
        if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
            throw "Go 1.26.5+ is required to build from source (https://go.dev/dl/)."
        }

        $tempDir = Join-Path ([IO.Path]::GetTempPath()) "ytd-install-$([guid]::NewGuid().ToString('N'))"
        New-Item -ItemType Directory -Force -Path $tempDir | Out-Null
        $archive = Join-Path $tempDir "source.zip"
        Write-Host "ytd: downloading source from GitHub..."
        Invoke-WebRequest -UseBasicParsing `
            -Uri "https://github.com/HectorYanes/ytd/archive/refs/heads/main.zip" `
            -OutFile $archive
        Expand-Archive -LiteralPath $archive -DestinationPath $tempDir -Force
        $repoDir = Join-Path $tempDir "ytd-main"
        if (-not (Test-Path (Join-Path $repoDir "go.mod"))) {
            throw "The downloaded source archive did not contain go.mod."
        }
    }

    Push-Location $repoDir
    try {
        $prefix = if ($env:PREFIX) { $env:PREFIX } else { Join-Path $env:LOCALAPPDATA "ytd" }
        $binDir = Join-Path $prefix "bin"
        $version = "dev"
        if (Get-Command git -ErrorAction SilentlyContinue) {
            $gitVersion = git describe --tags --always --dirty 2>$null
            if ($LASTEXITCODE -eq 0 -and $gitVersion) { $version = $gitVersion }
        }

        if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
            throw "Go 1.26.5+ is required to build from source (https://go.dev/dl/)."
        }

        Write-Host "ytd: building $version for windows/$env:PROCESSOR_ARCHITECTURE..."
        New-Item -ItemType Directory -Force -Path $binDir | Out-Null
        go build -trimpath -ldflags "-s -w -X main.version=$version" -o (Join-Path $binDir "ytd.exe") ./cmd/ytd
        if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE." }

        if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
            Write-Warning "ffmpeg is missing: merging separate video+audio streams will fail. Install it separately, for example: winget install Gyan.FFmpeg"
        }

        $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
        if ($userPath -notlike "*$binDir*") {
            [Environment]::SetEnvironmentVariable("Path", "$userPath;$binDir", "User")
            Write-Host "ytd: added $binDir to the user PATH (open a new terminal)."
        }

        Write-Host "ytd: installed $binDir\ytd.exe"
        & (Join-Path $binDir "ytd.exe") --update
        & (Join-Path $binDir "ytd.exe") --check
    }
    finally {
        Pop-Location
    }
}
finally {
    if ($tempDir -and (Test-Path $tempDir)) {
        Remove-Item -LiteralPath $tempDir -Recurse -Force
    }
}
