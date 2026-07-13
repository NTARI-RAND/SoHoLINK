# Dynamic DNS for the SPIRE control plane (TODO 37)

`spire.soholink.org` is served by a **direct port-forward**, not the Cloudflare
tunnel: router `WAN:8081` → this host's LAN IP → the Docker-published
`spire-server:8081`. Member agents dial `spire.soholink.org:8081` and SPIRE's own
TLS rides end-to-end (no proxy terminates it), so **all device classes** — phones
and Smart TVs included — can attest with no client-side software.

Because the record must point straight at the home IP, it is **DNS-only
(unproxied / grey-cloud)**: Cloudflare's proxy does not carry raw TCP 8081. That
publicly exposes the public IP by design; the security boundary for `:8081` is
SPIRE **join-token attestation**, not network obscurity.

Spectrum residential IPs are dynamic, so `update-spire-dns.ps1` reconciles the A
record on a schedule (create-on-first-run, update-on-change, no-op otherwise).

## One-time setup on NTARIHQ

1. Create a Cloudflare API token scoped **Zone → DNS → Edit** on `soholink.org`.
2. Save it out of the repo:
   ```powershell
   New-Item -ItemType Directory -Force C:\ProgramData\SoHoLINK | Out-Null
   Set-Content C:\ProgramData\SoHoLINK\cf-ddns-token.txt "<token>" -NoNewline
   ```
3. First run (also creates the record if absent):
   ```powershell
   pwsh -File deploy\ddns\update-spire-dns.ps1
   ```
   Expect `CREATED spire.soholink.org -> <public-ip> (DNS-only)` in
   `C:\ProgramData\SoHoLINK\ddns-spire.log`.
4. Schedule it every 5 minutes (run as SYSTEM so it survives logoff):
   ```powershell
   $action  = New-ScheduledTaskAction -Execute "pwsh.exe" `
     -Argument '-NoProfile -File "C:\Users\<user>\Documents\NTARI Official Docs\Development\Substrate\SoHoLINK\deploy\ddns\update-spire-dns.ps1"'
   $trigger = New-ScheduledTaskTrigger -Once -At (Get-Date) `
     -RepetitionInterval (New-TimeSpan -Minutes 5)
   Register-ScheduledTask -TaskName "SoHoLINK SPIRE DDNS" -Action $action `
     -Trigger $trigger -User "SYSTEM" -RunLevel Highest
   ```

## Never commit the token

`C:\ProgramData\SoHoLINK\cf-ddns-token.txt` lives outside the repo on purpose.
The script also accepts `$env:CLOUDFLARE_API_TOKEN`. Neither path should ever be
checked in.

## Durability

Pair with SPIRE `x509pop` attestation (TODO 36) so node identity survives SVID
expiry without a one-time join token; the two together make the direct-serve path
operationally stable.
