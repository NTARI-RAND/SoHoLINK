#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Installs fedaaa.exe as a persistent Windows node service.

.DESCRIPTION
    - Copies fedaaa.exe to C:\Program Files\SoHoLINK\
    - Registers a Scheduled Task that starts the node at boot (as SYSTEM)
    - Opens Windows Firewall for RADIUS (UDP 1812/1813) and HTTP API (TCP 8080)
    - Starts the node immediately

.NOTES
    Must be run as Administrator.
    To uninstall: scripts\uninstall-service.ps1
#>

$ErrorActionPreference = "Stop"

$InstallDir  = "C:\Program Files\SoHoLINK"
$ExeName     = "fedaaa.exe"
$TaskName    = "SoHoLINK Node"
$ScriptRoot  = Split-Path -Parent $PSScriptRoot   # project root

Write-Host ""
Write-Host "  SoHoLINK Node Installer" -ForegroundColor Cyan
Write-Host "  ========================" -ForegroundColor Cyan
Write-Host ""

# ── 1. Locate the binary ─────────────────────────────────────────────────────
$SourceExe = Join-Path $ScriptRoot $ExeName
if (-not (Test-Path $SourceExe)) {
    # Try the project root directly (in case script is run from elsewhere)
    $SourceExe = Join-Path (Get-Location) $ExeName
}
if (-not (Test-Path $SourceExe)) {
    Write-Host "  ERROR: $ExeName not found." -ForegroundColor Red
    Write-Host "  Build it first:  go build -mod=mod -tags '!gui' -o fedaaa.exe ./cmd/fedaaa/..." -ForegroundColor Yellow
    exit 1
}
Write-Host "  [1/5] Found binary: $SourceExe" -ForegroundColor Green

# ── 2. Copy to install directory ─────────────────────────────────────────────
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}
Copy-Item -Path $SourceExe -Destination $InstallDir -Force
Write-Host "  [2/5] Installed to: $InstallDir\$ExeName" -ForegroundColor Green

# ── 3. Register Scheduled Task (runs as SYSTEM at every boot) ────────────────
$ExePath  = Join-Path $InstallDir $ExeName
$Action   = New-ScheduledTaskAction `
                -Execute $ExePath `
                -Argument "start" `
                -WorkingDirectory $InstallDir

$Trigger  = New-ScheduledTaskTrigger -AtStartup

$Settings = New-ScheduledTaskSettingsSet `
                -ExecutionTimeLimit ([TimeSpan]::Zero) `
                -RestartCount 5 `
                -RestartInterval (New-TimeSpan -Minutes 1) `
                -StartWhenAvailable

$Principal = New-ScheduledTaskPrincipal `
                -UserId "SYSTEM" `
                -LogonType ServiceAccount `
                -RunLevel Highest

# Remove existing task if present
Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue

Register-ScheduledTask `
    -TaskName  $TaskName `
    -Action    $Action `
    -Trigger   $Trigger `
    -Settings  $Settings `
    -Principal $Principal `
    -Force | Out-Null

Write-Host "  [3/5] Scheduled Task registered (starts at boot as SYSTEM)" -ForegroundColor Green

# ── 4. Open firewall ports ───────────────────────────────────────────────────
$rules = @(
    @{ Name="SoHoLINK RADIUS Auth";       Port=1812; Proto="UDP"; Dir="Inbound" },
    @{ Name="SoHoLINK RADIUS Accounting"; Port=1813; Proto="UDP"; Dir="Inbound" },
    @{ Name="SoHoLINK HTTP API";          Port=8080; Proto="TCP"; Dir="Inbound" }
)

foreach ($r in $rules) {
    Remove-NetFirewallRule -DisplayName $r.Name -ErrorAction SilentlyContinue
    New-NetFirewallRule `
        -DisplayName $r.Name `
        -Direction   $r.Dir `
        -Protocol    $r.Proto `
        -LocalPort   $r.Port `
        -Action      Allow `
        -Profile     Any | Out-Null
}
Write-Host "  [4/5] Firewall rules added (UDP 1812, UDP 1813, TCP 8080)" -ForegroundColor Green

# ── 5. Start the node now ────────────────────────────────────────────────────
Start-ScheduledTask -TaskName $TaskName
Start-Sleep -Seconds 2

$State = (Get-ScheduledTask -TaskName $TaskName).State
Write-Host "  [5/5] Node started — Task state: $State" -ForegroundColor Green

Write-Host ""
Write-Host "  Installation complete!" -ForegroundColor Cyan
Write-Host ""
Write-Host "  HTTP API   : http://localhost:8080/api/health"
Write-Host "  RADIUS     : UDP 0.0.0.0:1812  (auth)"
Write-Host "               UDP 0.0.0.0:1813  (accounting)"
Write-Host ""
Write-Host "  Manage via Task Scheduler -> '$TaskName'"
Write-Host "  Uninstall  : scripts\uninstall-service.ps1  (as Administrator)"
Write-Host ""
