#Requires -RunAsAdministrator
# update-binary.ps1 — stop service, replace binary, restart service
param(
    [string]$SourceExe = "C:\Users\Jodson Graves\Documents\SoHoLINK\fedaaa.exe",
    [string]$InstallDir = "C:\Program Files\SoHoLINK",
    [string]$TaskName = "SoHoLINK Node"
)

$ErrorActionPreference = "Stop"

Write-Host "Stopping $TaskName..." -ForegroundColor Yellow
Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2

Write-Host "Copying $SourceExe -> $InstallDir\fedaaa.exe" -ForegroundColor Cyan
Copy-Item -Path $SourceExe -Destination "$InstallDir\fedaaa.exe" -Force

$ver = & "$InstallDir\fedaaa.exe" --version 2>&1
Write-Host "Installed: $ver" -ForegroundColor Green

Write-Host "Starting $TaskName..." -ForegroundColor Yellow
Start-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2

$state = (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue).State
Write-Host "Task state: $state" -ForegroundColor Green
Write-Host "Done." -ForegroundColor Green
