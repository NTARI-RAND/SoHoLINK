#Requires -Version 5.1
<#
.SYNOPSIS
    SoHoLINK — single-command developer build script.

.DESCRIPTION
    Produces ALL platform packages (Windows .exe + NSIS installer, Linux
    .tar.gz + .deb + .rpm, macOS universal .tar.gz) in one run by invoking
    GoReleaser with the project's .goreleaser.yml.

    Prerequisites handled automatically:
      • GoReleaser  — installed via `go install` if absent
      • MinGW GCC   — checked; user prompted to install via winget if absent
      • NSIS        — checked; installer step skipped (not hard-fail) if absent
      • fyne CLI    — installed via `go install` if absent (for icon conversion)

    After a successful build, dist/ is opened in Explorer.

.PARAMETER Version
    Override the version string (default: reads from FyneApp.toml).

.PARAMETER SkipTests
    Skip `go test` before building.

.PARAMETER Release
    Run a real GoReleaser release (requires a git tag v*). Default: snapshot.

.PARAMETER NoSign
    Skip EV code signing of the Windows .exe/.msi artifacts in dist\.
    Without this flag, signing runs after the build but downgrades to a
    warning when the Sectigo token/cert or signtool is unavailable, so CI
    and dev machines without the token still build (see deploy/signing.md).

.EXAMPLE
    .\scripts\build-all.ps1
    .\scripts\build-all.ps1 -SkipTests
    .\scripts\build-all.ps1 -Release
    .\scripts\build-all.ps1 -NoSign

.NOTES
    Must be run from the repository root, or from the scripts\ subdirectory
    (the script auto-detects and changes to the repo root).
#>
param(
    [string] $Version    = "",
    [switch] $SkipTests  = $false,
    [switch] $Release    = $false,
    [switch] $NoSign     = $false
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# ── Colours ─────────────────────────────────────────────────────────────────
function Write-Header  { param($m) Write-Host "`n══ $m " -ForegroundColor Cyan }
function Write-Step    { param($m) Write-Host "  • $m"   -ForegroundColor Yellow }
function Write-Ok      { param($m) Write-Host "  ✔ $m"   -ForegroundColor Green }
function Write-Warn    { param($m) Write-Host "  ⚠ $m"   -ForegroundColor DarkYellow }
function Write-Fail    { param($m) Write-Host "  ✘ $m"   -ForegroundColor Red }

# ── Banner ───────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "╔══════════════════════════════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "║        SoHoLINK — All-Platform Release Builder                 ║" -ForegroundColor Cyan
Write-Host "║        Network Theory Applied Research Institute               ║" -ForegroundColor Cyan
Write-Host "╚══════════════════════════════════════════════════════════════════╝" -ForegroundColor Cyan

# ── Locate repo root ─────────────────────────────────────────────────────────
$ScriptDir  = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot   = if (Test-Path (Join-Path $ScriptDir ".goreleaser.yml")) {
                  $ScriptDir
              } else {
                  Split-Path -Parent $ScriptDir
              }
if (-not (Test-Path (Join-Path $RepoRoot ".goreleaser.yml"))) {
    Write-Fail ".goreleaser.yml not found. Run this script from the repo root."
    exit 1
}
Push-Location $RepoRoot
Write-Ok "Repo root: $RepoRoot"

try {

# ── Read version from FyneApp.toml if not overridden ────────────────────────
if ($Version -eq "") {
    $tomlPath = Join-Path $RepoRoot "FyneApp.toml"
    if (Test-Path $tomlPath) {
        $vLine = Select-String -Path $tomlPath -Pattern '^\s*Version\s*=' | Select-Object -First 1
        if ($vLine) {
            $Version = ($vLine.Line -split '"')[1]
        }
    }
    if ($Version -eq "") { $Version = "0.1.0" }
}
Write-Ok "Version: $Version"

# ════════════════════════════════════════════════════════════════════════════
# STEP 1 — Check prerequisites
# ════════════════════════════════════════════════════════════════════════════
Write-Header "Checking prerequisites"

# Go
$goCmd = Get-Command go -ErrorAction SilentlyContinue
if (-not $goCmd) {
    Write-Fail "Go is not installed. Download from https://go.dev/dl/"
    exit 1
}
$goVer = (& go version) -replace "go version go", "" -replace " .*", ""
Write-Ok "Go $goVer"

# MinGW GCC (needed for CGO Windows GUI build)
$gccCmd = Get-Command x86_64-w64-mingw32-gcc -ErrorAction SilentlyContinue
if (-not $gccCmd) {
    $gccCmd = Get-Command gcc -ErrorAction SilentlyContinue
}
if (-not $gccCmd) {
    Write-Warn "MinGW GCC not found — Windows GUI binary will be CGO_ENABLED=0 (no GUI)."
    Write-Warn "To fix: winget install msys2  →  pacman -S mingw-w64-x86_64-gcc"
    Write-Warn "Then add C:\msys64\mingw64\bin to your PATH."
    $HaveGCC = $false
} else {
    Write-Ok "GCC: $($gccCmd.Source)"
    $HaveGCC = $true
}

# NSIS (optional — installer step in .goreleaser.yml hook skips if absent)
$nsisCmd = Get-Command makensis -ErrorAction SilentlyContinue
if ($nsisCmd) {
    Write-Ok "NSIS: $($nsisCmd.Source)"
} else {
    Write-Warn "NSIS not found — Windows Setup.exe will be skipped (zip still produced)."
    Write-Warn "To fix: winget install NSIS.NSIS"
}

# GoReleaser
$grCmd = Get-Command goreleaser -ErrorAction SilentlyContinue
if (-not $grCmd) {
    Write-Step "Installing GoReleaser via go install..."
    & go install "github.com/goreleaser/goreleaser/v2@latest"
    if ($LASTEXITCODE -ne 0) { Write-Fail "GoReleaser install failed."; exit 1 }
    Write-Ok "GoReleaser installed."
} else {
    $grVer = (& goreleaser --version 2>&1 | Select-Object -First 1) -replace ".*goreleaser version ", ""
    Write-Ok "GoReleaser $grVer"
}

# fyne CLI (used for icon conversion + packaging)
$fyneCmd = Get-Command fyne -ErrorAction SilentlyContinue
if (-not $fyneCmd) {
    Write-Step "Installing fyne CLI via go install..."
    & go install "fyne.io/fyne/v2/cmd/fyne@latest"
    if ($LASTEXITCODE -ne 0) {
        Write-Warn "fyne install failed — icon conversion will be skipped."
    } else {
        Write-Ok "fyne CLI installed."
    }
}

# ════════════════════════════════════════════════════════════════════════════
# STEP 2 — Generate assets (logo.png, logo.ico) from logo.svg
# ════════════════════════════════════════════════════════════════════════════
Write-Header "Generating brand assets"

$svgPath = Join-Path $RepoRoot "assets\logo.svg"
$pngPath = Join-Path $RepoRoot "assets\logo.png"
$icoPath = Join-Path $RepoRoot "installer\windows\logo.ico"

if (-not (Test-Path $pngPath)) {
    # Try Inkscape first
    $inkscape = Get-Command inkscape -ErrorAction SilentlyContinue
    if ($inkscape) {
        Write-Step "Converting SVG → PNG via Inkscape..."
        & inkscape --export-type=png --export-filename="$pngPath" --export-width=512 "$svgPath" 2>$null
        if ($LASTEXITCODE -eq 0) { Write-Ok "assets\logo.png created." }
    }
    # Try rsvg-convert (usually available if Inkscape is not)
    if (-not (Test-Path $pngPath)) {
        $rsvg = Get-Command rsvg-convert -ErrorAction SilentlyContinue
        if ($rsvg) {
            Write-Step "Converting SVG → PNG via rsvg-convert..."
            & rsvg-convert -w 512 -h 512 "$svgPath" -o "$pngPath"
            if ($LASTEXITCODE -eq 0) { Write-Ok "assets\logo.png created." }
        }
    }
    if (-not (Test-Path $pngPath)) {
        Write-Warn "No SVG converter found (Inkscape / rsvg-convert). Skipping PNG generation."
        Write-Warn "FyneApp.toml expects assets\logo.png — supply this file manually for full packaging."
    }
} else {
    Write-Ok "assets\logo.png already exists."
}

# Generate .ico for NSIS (requires ImageMagick convert)
if (-not (Test-Path $icoPath) -and (Test-Path $pngPath)) {
    $magick = Get-Command magick -ErrorAction SilentlyContinue
    if (-not $magick) { $magick = Get-Command convert -ErrorAction SilentlyContinue }
    if ($magick) {
        Write-Step "Generating installer\windows\logo.ico via ImageMagick..."
        $null = New-Item -ItemType Directory -Force -Path (Split-Path $icoPath)
        & $magick.Source "$pngPath" -resize 256x256 "$icoPath" 2>$null
        if ($LASTEXITCODE -eq 0) { Write-Ok "logo.ico created." } else { Write-Warn "ICO generation failed — NSIS will use default icon." }
    } else {
        Write-Warn "ImageMagick not found — logo.ico not generated. NSIS may warn about missing icon."
    }
} elseif (Test-Path $icoPath) {
    Write-Ok "installer\windows\logo.ico already exists."
}

# NSIS placeholder bitmaps (wizard-banner.bmp, header.bmp) if absent
$nsisDir   = Join-Path $RepoRoot "installer\windows"
$bannerBmp = Join-Path $nsisDir "wizard-banner.bmp"
$headerBmp = Join-Path $nsisDir "header.bmp"

foreach ($bmp in @($bannerBmp, $headerBmp)) {
    if (-not (Test-Path $bmp)) {
        Write-Warn "$(Split-Path -Leaf $bmp) not found in installer\windows\ — NSIS may skip installer."
    }
}

# ════════════════════════════════════════════════════════════════════════════
# STEP 3 — Download Go module dependencies
# ════════════════════════════════════════════════════════════════════════════
Write-Header "Downloading Go dependencies"
Write-Step "go mod download..."
& go mod download
if ($LASTEXITCODE -ne 0) { Write-Fail "go mod download failed."; exit 1 }
Write-Ok "Dependencies ready."

# ════════════════════════════════════════════════════════════════════════════
# STEP 4 — Run tests (unless skipped)
# ════════════════════════════════════════════════════════════════════════════
if (-not $SkipTests) {
    Write-Header "Running tests"
    Write-Step "go test -short ./internal/..."
    & go test -short ./internal/...
    if ($LASTEXITCODE -ne 0) {
        Write-Fail "Tests failed. Fix failures or use -SkipTests to bypass."
        exit 1
    }
    Write-Ok "All tests passed."
} else {
    Write-Warn "Tests skipped (-SkipTests flag)."
}

# ════════════════════════════════════════════════════════════════════════════
# STEP 5 — GoReleaser build
# ════════════════════════════════════════════════════════════════════════════
Write-Header "Building all platform packages via GoReleaser"

$grArgs = if ($Release) {
    Write-Step "Mode: REAL RELEASE (git tag required)"
    @("release", "--clean")
} else {
    Write-Step "Mode: Snapshot (no git tag needed)"
    @("release", "--snapshot", "--clean")
}

# Set GOPATH so goreleaser hooks can reference it
$env:GOPATH = (& go env GOPATH)

Write-Step "goreleaser $($grArgs -join ' ')"
Write-Host ""

& goreleaser @grArgs

if ($LASTEXITCODE -ne 0) {
    Write-Fail "GoReleaser failed (exit code $LASTEXITCODE)."
    Write-Fail "Check the output above for details."
    exit 1
}

# ════════════════════════════════════════════════════════════════════════════
# STEP 6 — EV-sign Windows artifacts
# ════════════════════════════════════════════════════════════════════════════
Write-Header "EV code signing"

if ($NoSign) {
    Write-Warn "Signing skipped (-NoSign flag)."
} else {
    . (Join-Path $PSScriptRoot "codesign.ps1")
    $signables = @(Get-ChildItem -Path (Join-Path $RepoRoot "dist") -Recurse -File -ErrorAction SilentlyContinue |
                   Where-Object { $_.Extension -in @(".exe", ".msi") })
    if ($signables.Count -eq 0) {
        Write-Warn "No .exe/.msi artifacts found in dist\ — nothing to sign."
    } else {
        Write-Step "Signing $($signables.Count) artifact(s) with the Sectigo EV certificate..."
        $signed = Invoke-CodeSign -Path $signables.FullName -Description "SoHoLINK" `
            -ThumbprintFile (Join-Path $RepoRoot "certs\thumbprint.txt")
        if ($signed) {
            Write-Ok "$($signables.Count) artifact(s) EV-signed."
            if (Test-Path (Join-Path $RepoRoot "dist\soholink_${Version}_checksums.txt")) {
                Write-Warn "GoReleaser checksums were computed before signing — regenerate them for any signed artifact they cover."
            }
        }
    }
}

# ════════════════════════════════════════════════════════════════════════════
# STEP 7 — Summary
# ════════════════════════════════════════════════════════════════════════════
Write-Header "Build complete!"
Write-Host ""

$distDir = Join-Path $RepoRoot "dist"
$artifacts = Get-ChildItem -Path $distDir -Recurse -File |
             Where-Object { $_.Extension -in @(".zip", ".gz", ".deb", ".rpm", ".exe", ".AppImage", ".pkg") } |
             Sort-Object Length -Descending

if ($artifacts) {
    Write-Host "  Artifacts produced:" -ForegroundColor Green
    foreach ($f in $artifacts) {
        $size = if ($f.Length -ge 1MB) { "{0:N1} MB" -f ($f.Length / 1MB) }
                else { "{0:N0} KB" -f ($f.Length / 1KB) }
        Write-Host ("  {0,-55} {1,9}" -f $f.Name, $size) -ForegroundColor White
    }
} else {
    Write-Warn "No artifacts found in dist/ — check GoReleaser output."
}

$checksums = Join-Path $distDir "soholink_${Version}_checksums.txt"
if (Test-Path $checksums) {
    Write-Host ""
    Write-Host "  Checksums: $checksums" -ForegroundColor DarkGray
}

Write-Host ""
Write-Host "  dist\  →  " -NoNewline -ForegroundColor Gray
Write-Host $distDir -ForegroundColor Cyan

# Open dist\ in Explorer (Windows only)
if ($IsWindows -or (-not $PSVersionTable.Platform)) {
    Write-Host ""
    Write-Step "Opening dist\ in Explorer..."
    Start-Process explorer.exe $distDir
}

Write-Host ""
Write-Host "  To create a GitHub release:" -ForegroundColor DarkGray
Write-Host "    git tag v$Version && git push origin v$Version" -ForegroundColor DarkGray
Write-Host "    (CI will run GoReleaser in release mode automatically)" -ForegroundColor DarkGray
Write-Host ""

} finally {
    Pop-Location
}
