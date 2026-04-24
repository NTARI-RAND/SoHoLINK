//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func init() {
	detectTraditionalPrinters = detectTraditionalWindows
	detect3DPrinters = detect3DWindows
}


// runPowerShell executes a PowerShell command with a bounded timeout,
// profile loading disabled, and non-interactive mode. Returns trimmed
// stdout on success.
func runPowerShell(script string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("powershell: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// winPrinter mirrors the fields we consume from Get-Printer output.
type winPrinter struct {
	Name          string `json:"Name"`
	DriverName    string `json:"DriverName"`
	PortName      string `json:"PortName"`
	ShareName     string `json:"ShareName"`
	PrinterStatus int    `json:"PrinterStatus"`
	DeviceType    string `json:"DeviceType"`
	PNPDeviceID   string `json:"PNPDeviceID"`
}

func detectTraditionalWindows() ([]PrinterInfo, error) {
	out, err := runPowerShell(
		`Get-Printer | Select-Object Name,DriverName,PortName,ShareName,PrinterStatus,DeviceType,PNPDeviceID | ConvertTo-Json -Compress`,
		10*time.Second)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	// ConvertTo-Json emits an object (not an array) when there's exactly
	// one result. Normalize to an array.
	if !strings.HasPrefix(out, "[") {
		out = "[" + out + "]"
	}

	var raw []winPrinter
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("parse Get-Printer output: %w", err)
	}

	printers := make([]PrinterInfo, 0, len(raw))
	for _, p := range raw {
		id := p.PNPDeviceID
		if id == "" {
			id = "name:" + p.Name
		}
		printers = append(printers, PrinterInfo{
			ID:             id,
			Kind:           PrinterKindTraditional,
			Name:           p.Name,
			Model:          p.DriverName,
			ConnectionPath: p.ShareName,
		})
	}
	return printers, nil
}

// winSerialDevice mirrors fields from Get-PnpDevice for serial-capable
// USB devices.
type winSerialDevice struct {
	InstanceId   string `json:"InstanceId"`
	FriendlyName string `json:"FriendlyName"`
	Manufacturer string `json:"Manufacturer"`
	Status       string `json:"Status"`
}

func detect3DWindows() ([]PrinterInfo, error) {
	// Query USB and serial-port PnP devices. Filter to Present/OK only.
	script := `Get-PnpDevice -Class 'Ports','USB' -PresentOnly | ` +
		`Where-Object { $_.Status -eq 'OK' } | ` +
		`Select-Object InstanceId,FriendlyName,Manufacturer,Status | ConvertTo-Json -Compress`

	out, err := runPowerShell(script, 10*time.Second)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	if !strings.HasPrefix(out, "[") {
		out = "[" + out + "]"
	}

	var raw []winSerialDevice
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("parse Get-PnpDevice output: %w", err)
	}

	var printers []PrinterInfo
	for _, d := range raw {
		vid := extractVIDFromInstanceID(d.InstanceId)
		if vid == "" {
			continue
		}
		vendor, ok := known3DPrinterVendorIDs[vid]
		if !ok {
			continue
		}
		name := d.FriendlyName
		if name == "" {
			name = vendor + " device"
		}
		printers = append(printers, PrinterInfo{
			ID:    d.InstanceId,
			Kind:  PrinterKind3D,
			Name:  name,
			Model: vendor,
			// ConnectionPath left blank on Windows — COM port mapping
			// requires a separate WMI query and is resolved by the
			// print-service container at runtime.
		})
	}
	return printers, nil
}

// extractVIDFromInstanceID parses a Windows PnP InstanceId string like
//
//	"USB\VID_2C99&PID_0002\0123456"
//
// and returns the 4-hex-character vendor ID ("2C99"), or "" if the
// string is not in the expected format.
func extractVIDFromInstanceID(instanceID string) string {
	marker := "VID_"
	idx := strings.Index(instanceID, marker)
	if idx < 0 {
		return ""
	}
	start := idx + len(marker)
	if start+4 > len(instanceID) {
		return ""
	}
	return strings.ToUpper(instanceID[start : start+4])
}
