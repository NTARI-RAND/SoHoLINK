# build-gui.ps1
# Builds soholink.exe with GUI tag and version ldflags.
# Usage: .\scripts\build-gui.ps1 [-Version "0.1.1"] [-Commit "537b76f"] [-BuildDate "2026-03-06"]
param(
    [string]$Version   = "0.1.1",
    [string]$Commit    = "537b76f",
    [string]$BuildDate = "2026-03-06"
)

$ErrorActionPreference = "Stop"
$projectRoot = $PSScriptRoot | Split-Path
Set-Location $projectRoot

$env:PATH = "C:\msys64\mingw64\bin;" + $env:PATH

Write-Host "Building soholink.exe v$Version ($Commit $BuildDate)..."
& go build -tags gui `
    -ldflags "-s -w -H windowsgui -X main.version=$Version -X main.commit=$Commit -X main.buildTime=$BuildDate" `
    -o soholink.exe ./cmd/soholink/

if ($LASTEXITCODE -ne 0) {
    Write-Host "[ERROR] Build failed." -ForegroundColor Red
    exit 1
}

$size = [math]::Round((Get-Item "$projectRoot\soholink.exe").Length / 1MB, 1)
Write-Host "soholink.exe built OK ($size MB)" -ForegroundColor Green
