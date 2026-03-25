# sign-binary.ps1 — Sign SoHoLINK executables with the NTARI code signing certificate
#
# Usage:
#   .\scripts\sign-binary.ps1                    # sign all binaries in bin/
#   .\scripts\sign-binary.ps1 -BinaryPath .\bin\SoHoLINK.exe   # sign specific file
#
# For production, replace the self-signed cert with a CA-issued certificate:
#   1. Purchase from DigiCert, Sectigo, or GlobalSign (~$300/year)
#   2. Import the .pfx into Cert:\CurrentUser\My
#   3. Update certs\thumbprint.txt with the new thumbprint
param(
    [string]$BinaryPath = "",
    [string]$ThumbprintFile = "certs\thumbprint.txt",
    [string]$TimestampServer = "http://timestamp.digicert.com"
)

$ErrorActionPreference = "Stop"

# Load thumbprint
if (-not (Test-Path $ThumbprintFile)) {
    Write-Host "ERROR: No thumbprint file found at $ThumbprintFile" -ForegroundColor Red
    Write-Host "Run the certificate generation first (see scripts/generate-cert.ps1)" -ForegroundColor Yellow
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

    if ($result.Status -eq "Valid" -or $result.StatusMessage -match "chain.*root") {
        Write-Host " OK ($($result.Status))" -ForegroundColor Green
    } else {
        Write-Host " $($result.Status): $($result.StatusMessage)" -ForegroundColor Yellow
    }
}

Write-Host ""
Write-Host "Done. Signature shows publisher as:" -ForegroundColor Green
Write-Host "  $($cert.Subject)"
