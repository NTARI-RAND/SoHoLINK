# Code Signing Runbook

How to sign the SoHoLINK Windows agent MSI installer and distribute it to pilot users. Current approach is self-signing; the same workflow adapts to a commercial OV or EV certificate later.

## Context

Windows SmartScreen warns users when they try to install software from an unknown publisher — an installer that is either unsigned or signed with a certificate that has no reputation yet. SignPath Foundation, which provides free code signing for established open-source projects, denied NTARI's application in May 2026 citing insufficient external verification signals. The pragmatic path is self-signed for the pilot phase, with the option to migrate to a commercial cert later.

Self-signing does **not** bypass SmartScreen. A self-signed cert has zero reputation, same as a brand-new commercial OV cert would. What it buys you:

- A consistent publisher identity ("NTARI") in installer dialogs instead of "Unknown"
- A clean install experience for pilot users who import the cert into their Trusted Publishers store ahead of time
- A signing workflow that ports directly to a commercial cert later (swap the `.pfx` file used by `signtool`)

For Shenandoah Condos pilot scale (~10 households, hand-holdable), self-sign with the cert-import step is workable. If cert-import friction frustrates pilot users in practice, that's the signal to migrate to an EV (Extended Validation) commercial cert, which bypasses SmartScreen from day one for any installer signed with it.

## One-time setup (operator, on NTARIHQ)

### Generate the cert

Open PowerShell as administrator on NTARIHQ:

```powershell
$cert = New-SelfSignedCertificate `
    -Type CodeSigningCert `
    -Subject "CN=Network Theory Applied Research Institute, O=NTARI, C=US" `
    -CertStoreLocation "Cert:\CurrentUser\My" `
    -KeyUsage DigitalSignature `
    -KeyAlgorithm RSA `
    -KeyLength 4096 `
    -NotAfter (Get-Date).AddYears(3) `
    -FriendlyName "NTARI SoHoLINK Code Signing"

$cert.Thumbprint
```

Save the thumbprint output for reference.

### Export the certs

Public cert (`.cer`) — what pilot users will import:

```powershell
Export-Certificate `
    -Cert $cert `
    -FilePath "D:\SoHoLINK-signing\ntari-codesign.cer"
```

Private cert (`.pfx`) — what `signtool` uses to sign MSI builds. Password-protect it:

```powershell
$password = Read-Host -Prompt "Strong password for .pfx" -AsSecureString
Export-PfxCertificate `
    -Cert $cert `
    -FilePath "D:\SoHoLINK-signing\ntari-codesign.pfx" `
    -Password $password
```

### Secure the private cert

The `.pfx` plus its password are the signing capability. Anyone with both can sign code as "NTARI."

- Store the `.pfx` on encrypted storage (BitLocker, etc.)
- Store the password separately in a password manager (1Password, Bitwarden, etc.)
- Do NOT commit either to git
- `*.pfx` should be in `.gitignore`

## Per-release: signing an MSI

`signtool.exe` is part of the Windows SDK. Typical path: `C:\Program Files (x86)\Windows Kits\10\bin\<version>\x64\signtool.exe`. Install the Windows 10/11 SDK from Microsoft if it's missing.

### Sign with timestamping

Timestamping ensures the signature remains valid after the cert expires. DigiCert's TSA is free and public (Sectigo's is an alternative).

```powershell
signtool sign `
    /f "D:\SoHoLINK-signing\ntari-codesign.pfx" `
    /p "PASSWORD_HERE" `
    /tr "http://timestamp.digicert.com" `
    /td SHA256 `
    /fd SHA256 `
    /d "SoHoLINK Agent" `
    "D:\path\to\SoHoLINK-Setup.msi"
```

`/d "SoHoLINK Agent"` sets the description shown in UAC prompts.

### Verify the signature

```powershell
signtool verify /pa /v "D:\path\to\SoHoLINK-Setup.msi"
```

Successful output shows the signature, the timestamp, and validates the cert chain. Expect a warning that the root cert is not trusted by default — that's correct for a self-signed cert and is what the user-side import step addresses.

### Test on a clean machine before shipping

On a Windows test machine or VM that has never seen this cert:

1. Copy the signed MSI, the `.cer`, and the install scripts (see Distribution below)
2. Run the install bat wrapper
3. Confirm the install completes without SmartScreen prompts after cert import
4. Confirm the agent launches and connects to soholink.org

If this passes, the release is ready for pilot distribution.

## Distribution to pilot users

Ship a folder containing:

1. `SoHoLINK-Setup.msi` — the signed installer
2. `ntari-codesign.cer` — the public cert for import
3. `install-soholink.ps1` — PowerShell install script (below)
4. `install-soholink.bat` — double-click wrapper (below)
5. A short README pointing users to the install bat

### install-soholink.ps1

```powershell
# install-soholink.ps1 — pilot user installer for SoHoLINK Agent
# Imports the NTARI code signing certificate and launches the MSI installer.

$ErrorActionPreference = "Stop"
$here = Split-Path -Parent $MyInvocation.MyCommand.Path

Write-Host "Importing NTARI code signing certificate..."

# Trusted Root: lets Windows trust the self-signed cert as a CA
Import-Certificate `
    -FilePath "$here\ntari-codesign.cer" `
    -CertStoreLocation Cert:\CurrentUser\Root | Out-Null

# Trusted Publisher: signed code from NTARI is auto-trusted
Import-Certificate `
    -FilePath "$here\ntari-codesign.cer" `
    -CertStoreLocation Cert:\CurrentUser\TrustedPublisher | Out-Null

Write-Host "Certificate imported. Launching SoHoLINK installer..."

Start-Process msiexec.exe `
    -ArgumentList "/i `"$here\SoHoLINK-Setup.msi`"" `
    -Wait

Write-Host "Install complete."
```

### install-soholink.bat

PowerShell execution policy can block `.ps1` files on user machines. The `.bat` wrapper invokes PowerShell with policy bypass for this single execution:

```batch
@echo off
powershell.exe -ExecutionPolicy Bypass -NoProfile -File "%~dp0install-soholink.ps1"
pause
```

Users double-click `install-soholink.bat` to run the installer.

### What pilot users will see

Running the `.bat`:

1. PowerShell window opens
2. "Importing NTARI code signing certificate..." appears
3. Windows shows a security prompt: "You are about to install a certificate from a certification authority claiming to represent Network Theory Applied Research Institute..." — user clicks **Yes**. (This happens once. The CurrentUser scope means it only affects this user, not the whole machine.)
4. "Launching SoHoLINK installer..."
5. MSI installer wizard appears; UAC prompt shows publisher = "Network Theory Applied Research Institute"
6. User clicks **Yes** on UAC, walks through the installer

If the user skips the `.bat` and double-clicks the MSI directly: SmartScreen shows "Windows protected your PC"; clicking **More info** reveals publisher = "Network Theory Applied Research Institute"; clicking **Run anyway** proceeds. Less smooth but functional.

### Security note for pilot users

Importing a self-signed cert into Trusted Root is a significant trust action. The cert can be used to sign any executable as "NTARI"; if the NTARI signing key were compromised, an attacker could sign malware that the user's machine would auto-trust. The same risk applies to any commercial code-signing cert NTARI might use later — the difference is that commercial CAs validate the issuing party's identity, whereas a self-signed cert has no external validation.

For the Shenandoah pilot specifically, users are trusting NTARI directly because the pilot relationship is direct. For broader distribution this trust model doesn't scale, which is part of why a commercial cert (preferably EV) is the right destination state.

## Future: migrating to a commercial cert

When NTARI acquires a commercial OV or EV code-signing certificate, the workflow above stays nearly identical:

- Generation is replaced by the commercial CA's process (CSR generation, identity verification, cert issuance)
- For **OV certs**: same `.pfx` workflow, just point `signtool` at the new cert file. Reputation accumulates as users install benign builds — not a day-one fix
- For **EV certs**: the private key lives on a hardware token (USB-attached HSM, e.g. YubiKey or vendor-provided token). `signtool sign /n "Network Theory Applied Research Institute" ...` finds the cert in the Windows cert store; signtool prompts for the hardware token PIN at signing time. Drop `/f` and `/p`. EV certs are immediately trusted by SmartScreen — no reputation period
- User-side install script and `.bat` become unnecessary. Commercial certs chain to roots already trusted on Windows; users don't need to import anything

When the migration happens, the user-facing distribution can collapse to just the signed MSI; the rest of this runbook becomes operator-only documentation.

## Troubleshooting

**`signtool: command not found`** — install the Windows 10/11 SDK from Microsoft, or add the SDK bin path to PATH.

**`SignerSign() failed: 0x80092009`** — the `.pfx` password is wrong, or the file is corrupted.

**Signature verifies but SmartScreen still warns** — for self-signed certs this never improves (SmartScreen explicitly does not extend reputation to non-CA-issued certs). The Trusted Publisher import is what suppresses the warning, not reputation. Verify the user actually ran the install bat, not just double-clicked the MSI directly.

**Pilot user reports "publisher not trusted" despite running the install script** — verify the cert was imported correctly:

```powershell
Get-ChildItem Cert:\CurrentUser\TrustedPublisher | Where-Object { $_.Subject -match "NTARI" }
Get-ChildItem Cert:\CurrentUser\Root | Where-Object { $_.Subject -match "NTARI" }
```

Both should return the cert. If either is missing, re-run the install script. If still missing, check that the user didn't decline the security prompt during cert import.
