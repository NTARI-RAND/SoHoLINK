package agent

import (
	"errors"
	"fmt"
)

// PrinterKind distinguishes printer categories for job routing.
type PrinterKind string

const (
	PrinterKindTraditional PrinterKind = "traditional"
	PrinterKind3D          PrinterKind = "3d"
)

// PrinterInfo describes a single printer detected on the host. The ID
// must be stable across reboots so contributor opt-out preferences
// persist reliably.
type PrinterInfo struct {
	// ID is a stable, platform-derived identifier. For traditional
	// printers on Windows this is the WMI PNPDeviceID or Name fallback;
	// on Unix it is the CUPS queue name. For 3D printers it is the USB
	// serial number when available, with a vendor/product fallback.
	ID string `json:"id"`

	// Kind is traditional or 3d.
	Kind PrinterKind `json:"kind"`

	// Name is the human-readable display name surfaced to the contributor.
	Name string `json:"name"`

	// Driver or model string when available. Optional.
	Model string `json:"model,omitempty"`

	// ConnectionPath describes how the agent would reach this printer
	// from inside a workload container. For traditional printers this
	// is typically the CUPS socket path or a Windows printer share name;
	// for 3D printers it is the serial device path (e.g. /dev/ttyUSB0,
	// COM3). Populated only when the agent can determine it without
	// requiring elevated access.
	ConnectionPath string `json:"connection_path,omitempty"`
}

// Platform-specific detection functions. Assigned in printers_windows.go
// and printers_unix.go via build tags. Tests may swap these to control
// detection output without touching platform code.
var (
	detectTraditionalPrinters func() ([]PrinterInfo, error)
	detect3DPrinters          func() ([]PrinterInfo, error)
)

// known3DPrinterVendorIDs is a non-exhaustive list of USB vendor IDs
// (uppercase hex, 4 chars) commonly found on consumer and hobbyist 3D
// printers. The list intentionally includes generic USB-to-serial
// converters (CH340, FTDI, CP210x) since many budget 3D printers use
// them; this accepts some false positives (Arduinos, other serial
// devices) that the contributor can opt out of per-device.
var known3DPrinterVendorIDs = map[string]string{
	"2C99": "Prusa Research",
	"1A86": "QinHeng (CH340)",
	"0403": "FTDI",
	"10C4": "Silicon Labs (CP210x)",
	"0483": "STMicroelectronics",
	"2E8A": "Raspberry Pi (Pico-based boards)",
	"1D6B": "Linux Foundation",
	"2D56": "BambuLab",
	"1F3A": "Creality",
	"0525": "NetChip (Ultimaker)",
	"2341": "Arduino (used in some printers)",
}

// ErrPrinterDetectionPartial indicates that one category of printer
// detection succeeded while another failed. The returned slice contains
// whatever printers were successfully detected; the error wraps the
// underlying detection failures for logging.
var ErrPrinterDetectionPartial = errors.New("printer detection partially failed")

// DetectPrinters enumerates traditional and 3D printers attached to the
// host. Failure in one category does not suppress results from the other;
// in partial-failure cases, the returned slice contains what was found
// and the returned error wraps the underlying causes.
//
// The agent must treat a non-nil error as a logged warning, not a fatal
// startup condition. A machine with no printers is a normal state and
// returns (nil, nil).
func DetectPrinters() ([]PrinterInfo, error) {
	var printers []PrinterInfo
	var trErr, tdErr error

	if detectTraditionalPrinters != nil {
		tr, err := detectTraditionalPrinters()
		if err != nil {
			trErr = fmt.Errorf("traditional: %w", err)
		}
		printers = append(printers, tr...)
	}

	if detect3DPrinters != nil {
		td, err := detect3DPrinters()
		if err != nil {
			tdErr = fmt.Errorf("3d: %w", err)
		}
		printers = append(printers, td...)
	}

	switch {
	case trErr != nil && tdErr != nil:
		return printers, fmt.Errorf("%w: %v; %v", ErrPrinterDetectionPartial, trErr, tdErr)
	case trErr != nil:
		return printers, fmt.Errorf("%w: %v", ErrPrinterDetectionPartial, trErr)
	case tdErr != nil:
		return printers, fmt.Errorf("%w: %v", ErrPrinterDetectionPartial, tdErr)
	}
	return printers, nil
}
