# trust-ntari-cert.ps1 — Install the NTARI code signing certificate as trusted
#
# Run this ONCE on any machine where you want SoHoLINK to be recognized
# without the "Unknown publisher" warning.
#
# Must be run as Administrator:
#   Right-click PowerShell → "Run as administrator"
#   cd C:\path\to\SoHoLINK
#   .\scripts\trust-ntari-cert.ps1
#
# What this does:
#   Adds the NTARI self-signed certificate to the Trusted Root store,
#   so Windows recognizes binaries signed by it as trusted.
#
# NOTE: For public distribution, purchase a real CA-issued certificate
#       so end users don't need to run this step.
param(
    [string]$CertPath = "certs\ntari-codesign.cer"
)

$ErrorActionPreference = "Stop"

# Check admin privileges
$isAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Host "ERROR: This script must be run as Administrator." -ForegroundColor Red
    Write-Host ""
    Write-Host "Right-click PowerShell and select 'Run as administrator', then try again." -ForegroundColor Yellow
    exit 1
}

if (-not (Test-Path $CertPath)) {
    Write-Host "ERROR: Certificate not found at $CertPath" -ForegroundColor Red
    exit 1
}

Write-Host "Installing NTARI code signing certificate as trusted..." -ForegroundColor Cyan
Write-Host "  Certificate: $CertPath"
Write-Host ""

# Import into Trusted Root CA store (machine-wide)
$cert = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2($CertPath)
Write-Host "  Subject:  $($cert.Subject)"
Write-Host "  Expires:  $($cert.NotAfter)"
Write-Host "  Thumbprint: $($cert.Thumbprint)"
Write-Host ""

$store = New-Object System.Security.Cryptography.X509Certificates.X509Store("Root", "LocalMachine")
$store.Open("ReadWrite")
$store.Add($cert)
$store.Close()

Write-Host "Certificate installed successfully." -ForegroundColor Green
Write-Host ""
Write-Host "SoHoLINK binaries signed by NTARI will now be recognized as trusted."
Write-Host "You can verify by right-clicking SoHoLINK.exe → Properties → Digital Signatures."
