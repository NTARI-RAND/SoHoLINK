//go:build !windows

package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

func init() {
	detectTraditionalPrinters = detectTraditionalUnix
	detect3DPrinters = detect3DUnix
}

// runCmd executes a command with a bounded timeout and returns trimmed
// stdout on success.
func runCmd(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// detectTraditionalUnix uses `lpstat -p -l` which works on both Linux
// and macOS wherever CUPS is installed. A missing lpstat or a CUPS
// daemon that is not running is treated as "no printers", not an
// error — many machines have no CUPS setup at all.
func detectTraditionalUnix() ([]PrinterInfo, error) {
	// Check for lpstat availability; if not present, that is not an error.
	if _, err := exec.LookPath("lpstat"); err != nil {
		return nil, nil
	}

	out, err := runCmd(10*time.Second, "lpstat", "-p", "-l")
	if err != nil {
		// lpstat returns non-zero when CUPS is not running or there are
		// no printers. Treat as empty, not an error.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, nil
		}
		return nil, err
	}
	return parseLpstatOutput(out), nil
}

// parseLpstatOutput parses the output of `lpstat -p -l` into a slice of
// PrinterInfo. Extracted for unit testing without invoking lpstat.
func parseLpstatOutput(out string) []PrinterInfo {
	if out == "" {
		return nil
	}
	var printers []PrinterInfo
	var current *PrinterInfo
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(line, "printer ") {
			if current != nil {
				printers = append(printers, *current)
			}
			parts := strings.SplitN(line, " ", 3)
			if len(parts) < 2 {
				current = nil
				continue
			}
			name := parts[1]
			current = &PrinterInfo{
				ID:   "cups:" + name,
				Kind: PrinterKindTraditional,
				Name: name,
			}
			continue
		}

		if current == nil {
			continue
		}

		if strings.HasPrefix(trimmed, "Description:") {
			current.Model = strings.TrimSpace(strings.TrimPrefix(trimmed, "Description:"))
		}
	}
	if current != nil {
		printers = append(printers, *current)
	}
	return printers
}

// detect3DUnix dispatches to the platform-specific serial enumeration
// strategy. Linux uses /dev/serial/by-id; macOS uses ioreg.
func detect3DUnix() ([]PrinterInfo, error) {
	switch runtime.GOOS {
	case "linux":
		return detect3DLinux()
	case "darwin":
		return detect3DDarwin()
	default:
		return nil, nil
	}
}

// detect3DLinux enumerates /dev/serial/by-id, which udev populates with
// stable symlinks whose filenames encode vendor/product/serial info.
// Example filename:
//
//	usb-Prusa_Research__prusa3d.com__Original_Prusa_i3_MK3_CZPX1234-if00
//
// The VID/PID are not in the filename; we must resolve the symlink target
// and read the corresponding sysfs attributes.
func detect3DLinux() ([]PrinterInfo, error) {
	const dir = "/dev/serial/by-id"
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	var printers []PrinterInfo
	for _, e := range entries {
		link := filepath.Join(dir, e.Name())
		target, err := filepath.EvalSymlinks(link)
		if err != nil {
			continue
		}
		vid, _ := readSysfsUSBID(target, "idVendor")
		if vid == "" {
			continue
		}
		vid = strings.ToUpper(vid)
		vendor, ok := known3DPrinterVendorIDs[vid]
		if !ok {
			continue
		}
		printers = append(printers, PrinterInfo{
			ID:             "usb:" + e.Name(),
			Kind:           PrinterKind3D,
			Name:           humanizeLinuxSerialName(e.Name()),
			Model:          vendor,
			ConnectionPath: target,
		})
	}
	return printers, nil
}

// readSysfsUSBID walks up from a /dev/tty* target to the USB device's
// sysfs directory and reads the given attribute (idVendor or idProduct).
// Returns an empty string and nil error if the attribute cannot be found.
func readSysfsUSBID(devPath, attr string) (string, error) {
	// Resolve /sys path for the device by walking /sys/class/tty/<name>.
	name := filepath.Base(devPath)
	sysLink := filepath.Join("/sys/class/tty", name, "device")
	sysPath, err := filepath.EvalSymlinks(sysLink)
	if err != nil {
		return "", nil
	}
	// Walk upward until we find the attribute file. USB devices have
	// idVendor a few levels up from the tty device.
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(sysPath, attr)
		if data, err := os.ReadFile(candidate); err == nil {
			return strings.TrimSpace(string(data)), nil
		}
		parent := filepath.Dir(sysPath)
		if parent == sysPath || parent == "/" {
			break
		}
		sysPath = parent
	}
	return "", nil
}

// humanizeLinuxSerialName strips the "usb-" prefix and "-ifNN" suffix
// from a /dev/serial/by-id entry to produce a cleaner display name.
func humanizeLinuxSerialName(raw string) string {
	name := strings.TrimPrefix(raw, "usb-")
	if idx := strings.LastIndex(name, "-if"); idx > 0 {
		name = name[:idx]
	}
	return strings.ReplaceAll(name, "_", " ")
}

// macIoregVIDRe extracts the vendor ID hex value from a line like
//
//	"idVendor" = 11366
//
// or
//
//	"idVendor" = 0x2C99
//
// ioreg emits decimal by default; convert to 4-char uppercase hex.
var macIoregVIDRe = regexp.MustCompile(`"idVendor"\s*=\s*(0x[0-9a-fA-F]+|\d+)`)
var macIoregProductRe = regexp.MustCompile(`"USB Product Name"\s*=\s*"([^"]+)"`)
var macIoregSerialRe = regexp.MustCompile(`"USB Serial Number"\s*=\s*"([^"]+)"`)

// detect3DDarwin parses `ioreg -p IOUSB -l` output for USB devices whose
// vendor ID matches known 3D printer makers.
func detect3DDarwin() ([]PrinterInfo, error) {
	if _, err := exec.LookPath("ioreg"); err != nil {
		return nil, nil
	}
	out, err := runCmd(10*time.Second, "ioreg", "-p", "IOUSB", "-l")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	// ioreg output is hierarchical; each device block ends with "}".
	// We split conservatively on "+-o " which marks the start of each
	// device entry in the tree.
	blocks := strings.Split(out, "+-o ")
	var printers []PrinterInfo
	for _, block := range blocks {
		vidMatch := macIoregVIDRe.FindStringSubmatch(block)
		if len(vidMatch) < 2 {
			continue
		}
		vid := normalizeIoregVID(vidMatch[1])
		if vid == "" {
			continue
		}
		vendor, ok := known3DPrinterVendorIDs[vid]
		if !ok {
			continue
		}

		var productName, serial string
		if m := macIoregProductRe.FindStringSubmatch(block); len(m) >= 2 {
			productName = m[1]
		}
		if m := macIoregSerialRe.FindStringSubmatch(block); len(m) >= 2 {
			serial = m[1]
		}

		id := "usb:" + vid
		if serial != "" {
			id = "usb:" + vid + ":" + serial
		}
		name := productName
		if name == "" {
			name = vendor + " device"
		}
		printers = append(printers, PrinterInfo{
			ID:    id,
			Kind:  PrinterKind3D,
			Name:  name,
			Model: vendor,
		})
	}
	return printers, nil
}

// normalizeIoregVID converts an ioreg idVendor value (decimal or hex)
// to a 4-character uppercase hex string.
func normalizeIoregVID(raw string) string {
	var n int64
	var err error
	if strings.HasPrefix(raw, "0x") || strings.HasPrefix(raw, "0X") {
		_, err = fmt.Sscanf(raw, "0x%x", &n)
	} else {
		_, err = fmt.Sscanf(raw, "%d", &n)
	}
	if err != nil || n <= 0 || n > 0xFFFF {
		return ""
	}
	return strings.ToUpper(fmt.Sprintf("%04x", n))
}
