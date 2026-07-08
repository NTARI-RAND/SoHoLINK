# codesign.ps1 — shared EV code-signing helpers for the SoHoLINK build scripts.
#
# Dot-source this file, then call Invoke-CodeSign on each .exe/.msi after it is
# produced:
#
#   . "$PSScriptRoot\codesign.ps1"
#   $signed = Invoke-CodeSign -Path $exePath -Description "SoHoLINK Agent" `
#       -ThumbprintFile (Join-Path $RepoRoot "certs\thumbprint.txt")
#
# Signing uses the NTARI Sectigo EV certificate (thumbprint in
# certs\thumbprint.txt; private key on the SafeNet/Thales USB token — Windows
# prompts for the token PIN on each signing operation; see deploy/signing.md).
#
# When the token/cert or signtool is unavailable, Invoke-CodeSign warns and
# returns $false so CI and dev machines without the token still build (their
# artifacts are simply unsigned). Pass -Require to hard-fail instead — use
# that for release builds that must never ship unsigned.

function Resolve-SignTool {
    # 1. PATH (NTARIHQ: User PATH extended by the NuGet buildtools install)
    $onPath = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($onPath) { return $onPath.Source }

    # 2. NuGet microsoft.windows.sdk.buildtools layout under %USERPROFILE%\.signtool
    #    (how signtool is provided on NTARIHQ — the Windows SDK proper is not
    #    installed there; see CLAUDE.md TODO 19), then the Windows 10/11 SDK kits.
    $roots = @(
        (Join-Path $env:USERPROFILE ".signtool"),
        "${env:ProgramFiles(x86)}\Windows Kits\10\bin",
        "${env:ProgramFiles}\Windows Kits\10\bin"
    )
    foreach ($root in $roots) {
        if (-not (Test-Path $root)) { continue }
        $hit = Get-ChildItem -Path $root -Recurse -Filter signtool.exe -ErrorAction SilentlyContinue |
            Where-Object { $_.FullName -like '*\x64\*' } |
            Sort-Object {
                # Prefer the newest SDK version; directory layout is <version>\x64\signtool.exe
                $v = $null
                if ([version]::TryParse($_.Directory.Parent.Name, [ref]$v)) { $v } else { [version]"0.0" }
            } -Descending |
            Select-Object -First 1
        if ($hit) { return $hit.FullName }
    }
    return $null
}

function Invoke-CodeSign {
    <#
    .SYNOPSIS
        EV-signs one or more .exe/.msi files with the NTARI Sectigo certificate.
    .OUTPUTS
        $true  — every file in -Path was signed.
        $false — signing was skipped (thumbprint file, cert/token, or signtool
                 absent); a warning was printed and the artifacts are unsigned.
        Throws when signtool itself fails on a file, or when prerequisites are
        missing and -Require was passed.
    #>
    param(
        [Parameter(Mandatory = $true)] [string[]] $Path,
        [string] $Description     = "",
        [string] $ThumbprintFile  = "certs\thumbprint.txt",
        [string] $TimestampServer = "http://timestamp.sectigo.com",
        [switch] $Require
    )

    $reason     = $null
    $thumbprint = $null

    if (-not (Test-Path $ThumbprintFile)) {
        $reason = "no thumbprint file at $ThumbprintFile (see deploy/signing.md)"
    }
    if (-not $reason) {
        $thumbprint = (Get-Content $ThumbprintFile -Raw).Trim()
        $cert = Get-ChildItem -Path "Cert:\CurrentUser\My" -ErrorAction SilentlyContinue |
                Where-Object { $_.Thumbprint -eq $thumbprint }
        if (-not $cert) {
            $reason = "EV cert $thumbprint not in Cert:\CurrentUser\My — is the USB token plugged in and SAC running?"
        }
    }
    $signtool = $null
    if (-not $reason) {
        $signtool = Resolve-SignTool
        if (-not $signtool) {
            $reason = "signtool.exe not found on PATH, under %USERPROFILE%\.signtool, or in Windows Kits 10"
        }
    }
    if ($reason) {
        if ($Require) { throw "Code signing required but unavailable: $reason" }
        Write-Warning "Skipping code signing: $reason. Artifacts will be UNSIGNED — do not distribute."
        return $false
    }

    foreach ($file in $Path) {
        Write-Host "    Signing $(Split-Path -Leaf $file) (token PIN prompt may appear)..."
        $sigArgs = @("sign", "/sha1", $thumbprint, "/fd", "SHA256", "/td", "SHA256", "/tr", $TimestampServer)
        if ($Description -ne "") { $sigArgs += @("/d", $Description) }
        $sigArgs += $file
        & $signtool @sigArgs
        if ($LASTEXITCODE -ne 0) { throw "signtool failed on $file (exit $LASTEXITCODE)" }
    }
    return $true
}
