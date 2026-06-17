# Code Signing Runbook

How to sign SoHoLINK Windows binaries and the MSI installer using the NTARI Sectigo EV certificate.

## Certificate

| Field | Value |
|---|---|
| Issuer | Sectigo Public Code Signing CA EV R36 |
| Subject | CN=Network Theory Applied Research Institute Inc, O=Network Theory Applied Research Institute Inc, S=Kentucky, C=US |
| Serial | 1268393 |
| Thumbprint | `8EE8F29DC1096452C2FF042C4549AE0E9E8921A1` |
| Expires | 2027-06-03 |
| Private key | SafeNet USB token (hardware-bound — key never leaves the token) |

The certificate chains to a Sectigo public root already trusted on all Windows machines. No end-user cert import is required.

## Prerequisites

- **SafeNet Authentication Client (SAC)** installed on the signing machine (NTARIHQ). SAC exposes the token's private key to Windows CNG so that `Set-AuthenticodeSignature` and `signtool` can use it.
- **USB token plugged in.** Windows will prompt for the token PIN on each signing operation. There is no way to automate this — CI signing is not supported.
- **EV cert visible in store.** Verify with:

```powershell
Get-ChildItem Cert:\CurrentUser\My | Where-Object { $_.Thumbprint -eq "8EE8F29DC1096452C2FF042C4549AE0E9E8921A1" }
```

`HasPrivateKey` must read `True`. If it reads `False`, SAC is not running or the token is not plugged in.

## Sign executables (PowerShell — recommended)

Use `scripts/sign-binary.ps1`. It reads the thumbprint from `certs/thumbprint.txt` and signs all `.exe` files in `bin/`, or a specific file if `-BinaryPath` is supplied.

```powershell
# Sign all binaries in bin/
.\scripts\sign-binary.ps1

# Sign a specific file
.\scripts\sign-binary.ps1 -BinaryPath .\bin\SoHoLINK.exe
```

Windows will prompt for the token PIN once per signing operation. The script exits non-zero on any signing failure.

## Sign the MSI installer (signtool)

`signtool.exe` is part of the Windows SDK (`C:\Program Files (x86)\Windows Kits\10\bin\<version>\x64\signtool.exe`).

For EV certs the private key is on the hardware token. Use `/n` (cert subject name) instead of `/f` + `/p`. Windows will prompt for the PIN.

```powershell
signtool sign `
    /n "Network Theory Applied Research Institute Inc" `
    /tr "http://timestamp.sectigo.com" `
    /td SHA256 `
    /fd SHA256 `
    /d "SoHoLINK Agent" `
    "D:\path\to\SoHoLINK-Setup.msi"
```

`/d "SoHoLINK Agent"` sets the description shown in UAC prompts.

## Verify the signature

```powershell
signtool verify /pa /v "D:\path\to\SoHoLINK-Setup.msi"
```

Successful output shows the signature, the Sectigo timestamp, and validates the full cert chain. No warnings about untrusted roots — the cert chains to a public Sectigo root that Windows already trusts.

## SmartScreen behavior

Microsoft removed EV's instant-SmartScreen advantage in March 2024. Both OV and EV signatures now build SmartScreen reputation organically through download volume — there is no instant-trust bypass. A freshly signed `SoHoLINK-Setup.msi` with no download history can still show a SmartScreen "unrecognized app" prompt on the first installs; this fades as install volume accrues against the publisher identity.

What the EV signature provides immediately: a valid, publicly-trusted signature chain and the real publisher name ("Network Theory Applied Research Institute Inc") in the UAC prompt — not the "Unknown publisher" warning an unsigned or self-signed build triggers. EV removes the unknown-publisher problem outright; SmartScreen reputation is a separate, gradual process.

## Distribution

Because the EV cert chains to a public root, the distribution bundle is just the signed MSI:

1. `SoHoLINK-Setup.msi` — the signed installer

No cert import step. No `.bat` wrapper. Users double-click the MSI.

## CI note

Signing requires the hardware token PIN. It cannot run unattended in GitHub Actions. Signing is an operator step performed on NTARIHQ immediately before a release is pushed to `web/static/`.

## Troubleshooting

**`HasPrivateKey: False`** — SAC is not running, or the token is not plugged in. Start SafeNet Authentication Client, insert the token, then re-run.

**`signtool: command not found`** — install the Windows 10/11 SDK from Microsoft, or add the SDK `x64` bin path to `PATH`.

**Signing fails with a CryptoAPI / `SignerSign()` error** — usually the token PIN was wrong or cancelled, or the token has locked after repeated wrong PINs. Open SAC to check token status and unlock if possible, then retry. A locked EV token cannot be reset — the key is non-recoverable and the cert must be reissued by Sectigo.

**Signature verifies but timestamp is missing** — the Sectigo TSA (`http://timestamp.sectigo.com`) was unreachable at signing time. The binary is signed but the signature will invalidate after the cert expires. Re-sign with a network connection.
