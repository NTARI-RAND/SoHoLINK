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
    [string]$Version   = "0.1.2",
    [string]$Commit    = "37a0756",
    [string]$BuildDate = "2026-03-08"
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

            // ICO directory entry is exactly 16 bytes:
            //   1 width | 1 height | 1 colors | 1 reserved
            //   2 planes | 2 bpp | 4 image-size | 4 file-offset
            // We store the first 8 fixed bytes here; size+offset are appended in the write loop.
            byte w = (sz == 256) ? (byte)0 : (byte)sz;
            byte h = (sz == 256) ? (byte)0 : (byte)sz;
            var hdr = new byte[8];
            hdr[0] = w;          // width  (0 means 256)
            hdr[1] = h;          // height (0 means 256)
            hdr[2] = 0;          // color count (0 = no palette)
            hdr[3] = 0;          // reserved
            hdr[4] = 1; hdr[5] = 0;   // color planes = 1
            hdr[6] = 32; hdr[7] = 0;  // bits per pixel = 32
            headers.Add(hdr);
        }

        // Layout: 6-byte ICO header + 16*n directory entries + image data blobs
        int offset = 6 + 16 * sizes.Length;
        using (var fs = new FileStream(outputPath, FileMode.Create)) {
            using (var bw = new BinaryWriter(fs)) {
                // ICO header (6 bytes)
                bw.Write((short)0);             // reserved
                bw.Write((short)1);             // type: 1 = ICO
                bw.Write((short)sizes.Length);  // image count

                // Directory entries — exactly 16 bytes each:
                //   8 (fixed hdr) + 4 (image size) + 4 (file offset) = 16
                for (int i = 0; i < sizes.Length; i++) {
                    bw.Write(headers[i]);                // 8 bytes
                    bw.Write((int)images[i].Length);     // 4 bytes — image data size
                    bw.Write((int)offset);               // 4 bytes — offset from file start
                    offset += images[i].Length;
                }

                // Image data blobs
                foreach (var img in images) bw.Write(img);
            }
        }
    }
}
'@ -ReferencedAssemblies "System.Drawing"

Write-Host "[2/3] Building ICO with sizes: 16, 20, 24, 32, 40, 48, 64, 96, 256 px"
# Full DPI coverage:
#   16 — taskbar / small icons at 100% DPI
#   20 — taskbar at 125% DPI
#   24 — taskbar at 150% DPI
#   32 — desktop shortcuts at 100% DPI
#   40 — taskbar at 200%+ DPI
#   48 — Explorer "Large Icons" view
#   64 — Explorer "Extra Large"
#   96 — very high-DPI (300%+) displays
#  256 — stored as PNG-in-ICO; used by Explorer "Extra Large" and Windows Store
[IcoWriter]::Write($src, @(16, 20, 24, 32, 40, 48, 64, 96, 256), $outPath)
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
