#Requires -Version 5.1
<#
.SYNOPSIS
    Builds the SoHoLINK Node Agent MSI installer.

.DESCRIPTION
    1. Cross-compiles the agent binary for windows/amd64
    2. EV-signs the agent + bundled SPIRE agent binaries
    3. Runs `wix build` to produce SoHoLINK.msi, then EV-signs the MSI

    Signing runs by default and downgrades to a warning when the Sectigo
    token/cert or signtool is unavailable, so CI and dev machines without
    the token still build (unsigned). Pass -Sign to REQUIRE signing (the
    build hard-fails if it cannot sign — use for release builds), or
    -NoSign to skip signing entirely. See deploy/signing.md.

.REQUIREMENTS
    - Go 1.24+ on PATH
    - WiX Toolset v4 (wix) on PATH: https://wixtoolset.org/
    - For signing: SafeNet USB token plugged in + signtool available
      (PATH, %USERPROFILE%\.signtool, or Windows Kits 10)
    - Run from the repository root or pass -RepoRoot

.EXAMPLE
    .\installer\windows\build.ps1
    .\installer\windows\build.ps1 -Version 2.1.0
    .\installer\windows\build.ps1 -Version 2.1.0 -Sign    # release: signing required
    .\installer\windows\build.ps1 -NoSign                 # explicit unsigned build
#>
param(
    [string]$RepoRoot  = (Resolve-Path "$PSScriptRoot\..\.." -ErrorAction Stop),
    [string]$Version   = "2.0.0",
    [string]$OutDir    = "$PSScriptRoot",
    [switch]$Sign,
    [switch]$NoSign
)

Set-StrictMode -Version Latest

$ErrorActionPreference = "Stop"

if ($Sign -and $NoSign) {
    throw "-Sign and -NoSign are mutually exclusive"
}

# Shared EV-signing helpers (Resolve-SignTool + Invoke-CodeSign)
. (Join-Path $RepoRoot "scripts\codesign.ps1")

Write-Host "==> Building SoHoLINK Node Agent MSI v$Version"
Write-Host "    Repo root : $RepoRoot"
Write-Host "    Output    : $OutDir"

# ── Step 0: generate WiX UI bitmap assets ──────────────────────────────────
Write-Host ""
Write-Host "==> Generating installer bitmap assets..."

Add-Type -AssemblyName System.Drawing

function New-SoHoLINKBmp {
    param([string]$Path, [int]$Width, [int]$Height, [int]$R, [int]$G, [int]$B)
    $bmp = New-Object System.Drawing.Bitmap $Width, $Height
    $gfx = [System.Drawing.Graphics]::FromImage($bmp)
    $gfx.Clear([System.Drawing.Color]::FromArgb($R, $G, $B))
    $gfx.Dispose()
    $bmp.Save($Path, [System.Drawing.Imaging.ImageFormat]::Bmp)
    $bmp.Dispose()
    Write-Host "    Created   : $Path"
}

# Interim near-white background (#F8F9FA) so wizard text (rendered black
# by Windows Installer) is legible. TODO 4 replaces these with branded artwork.
New-SoHoLINKBmp -Path "$PSScriptRoot\banner.bmp" -Width 493 -Height 58  -R 248 -G 249 -B 250
New-SoHoLINKBmp -Path "$PSScriptRoot\dialog.bmp" -Width 493 -Height 312 -R 248 -G 249 -B 250

# ── Step 0b: download SPIRE agent for Windows ──────────────────────────────
$spireAgentOut = Join-Path $PSScriptRoot "spire-agent.exe"
if (-not (Test-Path $spireAgentOut)) {
    Write-Host ""
    Write-Host "==> Downloading SPIRE 1.9.6 agent for Windows..."
    $spireZip = Join-Path $env:TEMP "spire-1.9.6-windows-amd64.zip"
    Invoke-WebRequest -Uri "https://github.com/spiffe/spire/releases/download/v1.9.6/spire-1.9.6-windows-amd64.zip" `
        -OutFile $spireZip -UseBasicParsing
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [System.IO.Compression.ZipFile]::OpenRead($spireZip)
    $entry = $zip.Entries | Where-Object { $_.Name -eq "spire-agent.exe" }
    [System.IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $spireAgentOut, $true)
    $zip.Dispose()
    Write-Host "    Agent     : $spireAgentOut"
} else {
    Write-Host "    Cached    : $spireAgentOut"
}

# ── Step 1: cross-compile agent for Windows amd64 ──────────────────────────
$agentOut = Join-Path $PSScriptRoot "soholink-agent.exe"
Write-Host ""
Write-Host "==> Compiling agent binary..."

# Resolve the allowlist public key for ldflags injection.
# Required for production (RELEASE=1) builds; dev builds may proceed without it,
# but the resulting agent will fail at first allowlist fetch with ErrAllowlistNoKey.
$AllowlistPublicKey = $env:ALLOWLIST_PUBLIC_KEY
if ([string]::IsNullOrWhiteSpace($AllowlistPublicKey)) {
    if ($env:RELEASE -eq "1") {
        Write-Error "ALLOWLIST_PUBLIC_KEY env var is required for RELEASE=1 builds"
        exit 1
    }
    Write-Warning "ALLOWLIST_PUBLIC_KEY not set; agent will fail at first allowlist fetch (dev build)"
    $AllowlistPublicKey = ""
}

$env:GOOS   = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"

$ldflagsValue = "-s -w -X main.version=$Version -X github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/agent.AllowlistPublicKey=$AllowlistPublicKey"

Push-Location $RepoRoot
try {
    & go build -ldflags $ldflagsValue -o $agentOut ./cmd/agent/...
    if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
} finally {
    Pop-Location
    Remove-Item Env:\GOOS   -ErrorAction SilentlyContinue
    Remove-Item Env:\GOARCH  -ErrorAction SilentlyContinue
    Remove-Item Env:\CGO_ENABLED -ErrorAction SilentlyContinue
}
Write-Host "    Binary    : $agentOut"

# Step 1b: EV-sign the agent binaries so the installed Windows service is
# signed. Runs by default; -NoSign skips, -Sign hard-fails when unavailable.
$signed = $false
if (-not $NoSign) {
    Write-Host ""
    Write-Host "==> EV-signing agent binary..."
    $signed = Invoke-CodeSign -Path $agentOut -Description "SoHoLINK Agent" `
        -ThumbprintFile "$RepoRoot\certs\thumbprint.txt" -Require:$Sign
    if ($signed) {
        Write-Host ""
        Write-Host "==> EV-signing bundled SPIRE agent..."
        $null = Invoke-CodeSign -Path $spireAgentOut -Description "SoHoLINK SPIRE Agent" `
            -ThumbprintFile "$RepoRoot\certs\thumbprint.txt" -Require:$Sign
    }
} else {
    Write-Host ""
    Write-Host "==> Signing skipped (-NoSign)."
}

# ── Step 2: build MSI with WiX ─────────────────────────────────────────────
$msiOut = Join-Path $OutDir "SoHoLINK-$Version.msi"
Write-Host ""
Write-Host "==> Running wix build..."

Push-Location $PSScriptRoot
try {
    & wix build SoHoLINK.wxs -o $msiOut -ext WixToolset.UI.wixext
    if ($LASTEXITCODE -ne 0) { throw "wix build failed with exit code $LASTEXITCODE" }
} finally {
    Pop-Location
}

# ── Step 3: copy MSI to web/static for portal download ─────────────────────
# Step 2b: EV-sign the MSI before publishing it. Gated on $signed so a build
# whose binaries were skipped (no token) does not emit a second skip warning.
if (-not $NoSign -and $signed) {
    Write-Host ""
    Write-Host "==> EV-signing MSI..."
    $null = Invoke-CodeSign -Path $msiOut -Description "SoHoLINK Agent Installer" `
        -ThumbprintFile "$RepoRoot\certs\thumbprint.txt" -Require:$Sign
}

$staticOut = Join-Path $RepoRoot "web\static\SoHoLINK-Setup.msi"
Copy-Item -Path $msiOut -Destination $staticOut -Force
Write-Host "    Static    : $staticOut"

Write-Host ""
Write-Host "==> Done: $msiOut"
