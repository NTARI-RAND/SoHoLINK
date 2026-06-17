# sign-binary.ps1 — Sign SoHoLINK executables with the NTARI Sectigo EV certificate
#
# Usage:
#   .\scripts\sign-binary.ps1                    # sign all binaries in bin/
#   .\scripts\sign-binary.ps1 -BinaryPath .\bin\SoHoLINK.exe   # sign specific file
#
# Prerequisites:
#   - SafeNet Authentication Client (SAC) installed and USB token plugged in
#   - Windows will prompt for the token PIN on each signing operation
#   - The EV cert (Sectigo, thumbprint in certs\thumbprint.txt) must be in Cert:\CurrentUser\My
param(
    [string]$BinaryPath = "",
    [string]$ThumbprintFile = "certs\thumbprint.txt",
    [string]$TimestampServer = "http://timestamp.sectigo.com"
)

$ErrorActionPreference = "Stop"

# Load thumbprint
if (-not (Test-Path $ThumbprintFile)) {
    Write-Host "ERROR: No thumbprint file found at $ThumbprintFile" -ForegroundColor Red
    Write-Host "Expected EV cert thumbprint at $ThumbprintFile (see deploy/signing.md)" -ForegroundColor Yellow
    exit 1
}

$thumbprint = (Get-Content $ThumbprintFile).Trim()
$cert = Get-ChildItem -Path "Cert:\CurrentUser\My" | Where-Object { $_.Thumbprint -eq $thumbprint }

if (-not $cert) {
    Write-Host "ERROR: Certificate with thumbprint $thumbprint not found in store" -ForegroundColor Red
    exit 1
}

Write-Host "Signing with: $($cert.Subject)" -ForegroundColor Cyan
Write-Host "Thumbprint:   $thumbprint"
Write-Host ""

# Determine which files to sign
if ($BinaryPath -ne "") {
    $files = @($BinaryPath)
} else {
    $files = Get-ChildItem -Path "bin\*.exe" | Select-Object -ExpandProperty FullName
}

foreach ($file in $files) {
    Write-Host "  Signing $file..." -NoNewline
    $result = Set-AuthenticodeSignature `
        -FilePath $file `
        -Certificate $cert `
        -TimestampServer $TimestampServer `
        -HashAlgorithm SHA256

    if ($result.Status -eq "Valid") {
        Write-Host " OK" -ForegroundColor Green
    } else {
        Write-Host " FAILED — $($result.Status): $($result.StatusMessage)" -ForegroundColor Red
        exit 1
    }
}

Write-Host ""
Write-Host "Done. Signature shows publisher as:" -ForegroundColor Green
Write-Host "  $($cert.Subject)"
