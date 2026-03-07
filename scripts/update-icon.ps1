# update-icon.ps1
# Converts assets/soholink-source.png → assets/soholink.ico (16/32/48/256 px)
# then rebuilds soholink.exe with the new embedded icon.
#
# Usage:
#   1. Save the new logo PNG to:  SoHoLINK\assets\soholink-source.png
#   2. Run:  .\scripts\update-icon.ps1
#   (must be run from the project root)

param(
    [string]$SourcePng = "assets\soholink-source.png",
    [string]$OutIco    = "assets\soholink.ico",
    [string]$Version   = "0.1.1",
    [string]$Commit    = "537b76f",
    [string]$BuildDate = "2026-03-06"
)

$ErrorActionPreference = "Stop"
$projectRoot = $PSScriptRoot | Split-Path

Set-Location $projectRoot

$sourcePath = Join-Path $projectRoot $SourcePng
$outPath    = Join-Path $projectRoot $OutIco

if (-not (Test-Path $sourcePath)) {
    Write-Host "[ERROR] Source PNG not found: $sourcePath" -ForegroundColor Red
    Write-Host "  Save the new logo to assets\soholink-source.png and re-run." -ForegroundColor Yellow
    exit 1
}

Write-Host "[1/3] Loading source image: $sourcePath"
Add-Type -AssemblyName System.Drawing
$src = [System.Drawing.Image]::FromFile($sourcePath)
Write-Host "      $($src.Width) x $($src.Height) px"

# ICO writer — uses C# 5-compatible syntax (traditional using blocks, no range operator)
Add-Type -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.Drawing;
using System.Drawing.Imaging;
using System.IO;

public static class IcoWriter {
    public static void Write(Image source, int[] sizes, string outputPath) {
        var images  = new List<byte[]>();
        var headers = new List<byte[]>();

        foreach (int sz in sizes) {
            var bmp = new Bitmap(sz, sz, PixelFormat.Format32bppArgb);
            using (var g = Graphics.FromImage(bmp)) {
                g.InterpolationMode  = System.Drawing.Drawing2D.InterpolationMode.HighQualityBicubic;
                g.CompositingQuality = System.Drawing.Drawing2D.CompositingQuality.HighQuality;
                g.SmoothingMode      = System.Drawing.Drawing2D.SmoothingMode.HighQuality;
                g.DrawImage(source, 0, 0, sz, sz);
            }

            byte[] imgBytes;
            if (sz == 256) {
                // 256px stored as PNG-in-ICO for best quality and small file size
                using (var ms = new MemoryStream()) {
                    bmp.Save(ms, ImageFormat.Png);
                    imgBytes = ms.ToArray();
                }
            } else {
                // Smaller sizes stored as 32bpp BMP-in-ICO (skip 14-byte BMP file header)
                using (var ms = new MemoryStream()) {
                    bmp.Save(ms, ImageFormat.Bmp);
                    byte[] raw = ms.ToArray();
                    imgBytes = new byte[raw.Length - 14];
                    Array.Copy(raw, 14, imgBytes, 0, imgBytes.Length);
                }
            }

            images.Add(imgBytes);
            bmp.Dispose();

            byte w = (sz == 256) ? (byte)0 : (byte)sz;
            byte h = (sz == 256) ? (byte)0 : (byte)sz;
            var hdr = new byte[16];
            hdr[0] = w;        // width  (0 = 256)
            hdr[1] = h;        // height (0 = 256)
            hdr[2] = 0;        // color count
            hdr[3] = 0;        // reserved
            hdr[4] = 1; hdr[5] = 0;   // color planes
            hdr[6] = 32; hdr[7] = 0;  // bits per pixel
            headers.Add(hdr);
        }

        // Layout: 6-byte ICO header + 16*n directory + image data
        int offset = 6 + 16 * sizes.Length;
        using (var fs = new FileStream(outputPath, FileMode.Create)) {
            using (var bw = new BinaryWriter(fs)) {
                // ICO header
                bw.Write((short)0);             // reserved
                bw.Write((short)1);             // type: ICO
                bw.Write((short)sizes.Length);  // image count

                // Directory entries
                for (int i = 0; i < sizes.Length; i++) {
                    bw.Write(headers[i]);                // 8 bytes of size/color info
                    bw.Write((int)images[i].Length);     // image data byte count
                    bw.Write((int)offset);               // offset from start of file
                    offset += images[i].Length;
                }

                // Image data blobs
                foreach (var img in images) bw.Write(img);
            }
        }
    }
}
'@ -ReferencedAssemblies "System.Drawing"

Write-Host "[2/3] Building ICO with sizes: 16, 32, 48, 256 px"
[IcoWriter]::Write($src, @(16, 32, 48, 256), $outPath)
$src.Dispose()

$kb = [math]::Round((Get-Item $outPath).Length / 1KB, 1)
Write-Host "      Written: $outPath ($kb KB)"

Write-Host "[3/3] Rebuilding soholink.exe with new icon (v$Version)..."
$env:PATH = "C:\msys64\mingw64\bin;" + $env:PATH
& go build -tags gui `
    -ldflags "-s -w -H windowsgui -X main.version=$Version -X main.commit=$Commit -X main.buildTime=$BuildDate" `
    -o soholink.exe ./cmd/soholink/

if ($LASTEXITCODE -ne 0) {
    Write-Host "[ERROR] Build failed." -ForegroundColor Red
    exit 1
}

Write-Host "      soholink.exe built OK"

# Refresh the desktop shortcut icon cache
$lnkPath = "$env:USERPROFILE\Desktop\SoHoLINK.lnk"
if (Test-Path $lnkPath) {
    $shell = New-Object -ComObject WScript.Shell
    $lnk   = $shell.CreateShortcut($lnkPath)
    $lnk.IconLocation = (Join-Path $projectRoot $OutIco)
    $lnk.Save()
    [System.Runtime.Interopservices.Marshal]::ReleaseComObject($shell) | Out-Null
    Write-Host "      Desktop shortcut icon updated."
}

Write-Host ""
Write-Host "Done! Icon updated to NTARI globe logo." -ForegroundColor Green
