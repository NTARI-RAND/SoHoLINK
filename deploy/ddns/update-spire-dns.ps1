<#
.SYNOPSIS
  Keeps spire.soholink.org pointed at NTARIHQ's current public IP (TODO 37).

.DESCRIPTION
  The SPIRE control plane is served by a direct port-forward (router WAN:8081 ->
  this host's LAN IP -> Docker-published spire-server:8081). Member agents dial
  spire.soholink.org:8081, so that name MUST resolve to the current public IP.
  Spectrum residential IPs are dynamic, so this script reconciles the A record
  on a schedule.

  The record is intentionally DNS-only (proxied = $false): Cloudflare's proxy does
  not carry raw TCP 8081, so the name must point directly at the home IP. This
  publicly exposes the public IP by design — the security boundary for :8081 is
  SPIRE join-token attestation, not network obscurity.

  Idempotent: creates the record on first run, updates only on change, no-ops when
  already correct. Safe to run every few minutes from Task Scheduler.

.NOTES
  Token: a Cloudflare API token scoped to Zone:DNS:Edit on soholink.org.
  Provided via $env:CLOUDFLARE_API_TOKEN, or a file at
  C:\ProgramData\SoHoLINK\cf-ddns-token.txt (kept OUT of the repo). Never commit it.
#>
[CmdletBinding()]
param(
  [string]$Hostname = "spire.soholink.org",
  [string]$ZoneName = "soholink.org",
  [string]$TokenPath = "C:\ProgramData\SoHoLINK\cf-ddns-token.txt",
  [string]$LogPath   = "C:\ProgramData\SoHoLINK\ddns-spire.log"
)

$ErrorActionPreference = "Stop"

function Write-Log($msg) {
  $line = "{0}  {1}" -f (Get-Date -Format "yyyy-MM-ddTHH:mm:ssK"), $msg
  try { $dir = Split-Path $LogPath; if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Force $dir | Out-Null }; Add-Content -Path $LogPath -Value $line } catch {}
  Write-Output $line
}

# --- token ---
$token = $env:CLOUDFLARE_API_TOKEN
if (-not $token -and (Test-Path $TokenPath)) { $token = (Get-Content $TokenPath -Raw).Trim() }
if (-not $token) { Write-Log "FATAL: no Cloudflare token (set CLOUDFLARE_API_TOKEN or place it at $TokenPath)"; exit 1 }
$headers = @{ Authorization = "Bearer $token"; "Content-Type" = "application/json" }
$api = "https://api.cloudflare.com/client/v4"

# --- current public IP ---
$ip = $null
foreach ($svc in @("https://api.ipify.org","https://ifconfig.me/ip","https://icanhazip.com")) {
  try { $ip = (Invoke-RestMethod -Uri $svc -TimeoutSec 10).ToString().Trim(); if ($ip -match '^\d{1,3}(\.\d{1,3}){3}$') { break } } catch {}
}
if (-not ($ip -match '^\d{1,3}(\.\d{1,3}){3}$')) { Write-Log "FATAL: could not determine public IPv4"; exit 1 }

# --- zone + record lookup ---
try {
  $zone = (Invoke-RestMethod -Uri "$api/zones?name=$ZoneName" -Headers $headers).result | Select-Object -First 1
  if (-not $zone) { Write-Log "FATAL: zone $ZoneName not visible to this token"; exit 1 }
  $rec = (Invoke-RestMethod -Uri "$api/zones/$($zone.id)/dns_records?type=A&name=$Hostname" -Headers $headers).result | Select-Object -First 1
} catch { Write-Log "FATAL: Cloudflare API error during lookup: $($_.Exception.Message)"; exit 1 }

$body = @{ type = "A"; name = $Hostname; content = $ip; ttl = 120; proxied = $false } | ConvertTo-Json

if (-not $rec) {
  try { Invoke-RestMethod -Method Post -Uri "$api/zones/$($zone.id)/dns_records" -Headers $headers -Body $body | Out-Null; Write-Log "CREATED $Hostname -> $ip (DNS-only)"; exit 0 }
  catch { Write-Log "FATAL: create failed: $($_.Exception.Message)"; exit 1 }
}

if ($rec.content -eq $ip -and $rec.proxied -eq $false) { Write-Log "OK: $Hostname already -> $ip"; exit 0 }

try {
  Invoke-RestMethod -Method Put -Uri "$api/zones/$($zone.id)/dns_records/$($rec.id)" -Headers $headers -Body $body | Out-Null
  Write-Log "UPDATED $Hostname: $($rec.content) (proxied=$($rec.proxied)) -> $ip (DNS-only)"; exit 0
} catch { Write-Log "FATAL: update failed: $($_.Exception.Message)"; exit 1 }
