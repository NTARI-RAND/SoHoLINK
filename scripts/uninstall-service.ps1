#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Removes the SoHoLINK node service installed by install-service.ps1.
#>

$ErrorActionPreference = "Stop"

$InstallDir = "C:\Program Files\SoHoLINK"
$TaskName   = "SoHoLINK Node"

Write-Host ""
Write-Host "  SoHoLINK Node Uninstaller" -ForegroundColor Yellow
Write-Host "  ==========================" -ForegroundColor Yellow
Write-Host ""

# Stop and remove scheduled task
Stop-ScheduledTask  -TaskName $TaskName -ErrorAction SilentlyContinue
Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
Write-Host "  [1/3] Scheduled Task removed" -ForegroundColor Green

# Remove firewall rules
@("SoHoLINK RADIUS Auth","SoHoLINK RADIUS Accounting","SoHoLINK HTTP API") | ForEach-Object {
    Remove-NetFirewallRule -DisplayName $_ -ErrorAction SilentlyContinue
}
Write-Host "  [2/3] Firewall rules removed" -ForegroundColor Green

# Remove install directory
if (Test-Path $InstallDir) {
    Remove-Item -Path $InstallDir -Recurse -Force
}
Write-Host "  [3/3] Removed $InstallDir" -ForegroundColor Green

Write-Host ""
Write-Host "  SoHoLINK node uninstalled." -ForegroundColor Yellow
Write-Host ""
