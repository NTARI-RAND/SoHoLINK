package agent

import (
	"errors"
	"testing"
)

func TestDetectPrinters_BothNil(t *testing.T) {
	origTr, origTd := detectTraditionalPrinters, detect3DPrinters
	t.Cleanup(func() {
		detectTraditionalPrinters = origTr
		detect3DPrinters = origTd
	})
	detectTraditionalPrinters = nil
	detect3DPrinters = nil

	printers, err := DetectPrinters()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if printers != nil {
		t.Fatalf("expected nil slice, got %v", printers)
	}
}

func TestDetectPrinters_BothSucceed(t *testing.T) {
	origTr, origTd := detectTraditionalPrinters, detect3DPrinters
	t.Cleanup(func() {
		detectTraditionalPrinters = origTr
		detect3DPrinters = origTd
	})
	detectTraditionalPrinters = func() ([]PrinterInfo, error) {
		return []PrinterInfo{{ID: "cups:laser", Kind: PrinterKindTraditional, Name: "laser"}}, nil
	}
	detect3DPrinters = func() ([]PrinterInfo, error) {
		return []PrinterInfo{{ID: "usb:2C99", Kind: PrinterKind3D, Name: "Prusa"}}, nil
	}

	printers, err := DetectPrinters()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(printers) != 2 {
		t.Fatalf("expected 2 printers, got %d", len(printers))
	}
}

func TestDetectPrinters_TraditionalFails3DSucceeds(t *testing.T) {
	origTr, origTd := detectTraditionalPrinters, detect3DPrinters
	t.Cleanup(func() {
		detectTraditionalPrinters = origTr
		detect3DPrinters = origTd
	})
	detectTraditionalPrinters = func() ([]PrinterInfo, error) {
		return nil, errors.New("lpstat exploded")
	}
	detect3DPrinters = func() ([]PrinterInfo, error) {
		return []PrinterInfo{{ID: "usb:2C99", Kind: PrinterKind3D, Name: "Prusa"}}, nil
	}

	printers, err := DetectPrinters()
	if !errors.Is(err, ErrPrinterDetectionPartial) {
		t.Fatalf("expected ErrPrinterDetectionPartial, got %v", err)
	}
	if len(printers) != 1 {
		t.Fatalf("expected 1 printer surviving partial failure, got %d", len(printers))
	}
	if printers[0].Kind != PrinterKind3D {
		t.Fatalf("expected 3D printer to survive, got %v", printers[0].Kind)
	}
}

func TestDetectPrinters_BothFail(t *testing.T) {
	origTr, origTd := detectTraditionalPrinters, detect3DPrinters
	t.Cleanup(func() {
		detectTraditionalPrinters = origTr
		detect3DPrinters = origTd
	})
	detectTraditionalPrinters = func() ([]PrinterInfo, error) {
		return nil, errors.New("lpstat exploded")
	}
	detect3DPrinters = func() ([]PrinterInfo, error) {
		return nil, errors.New("ioreg exploded")
	}

	printers, err := DetectPrinters()
	if !errors.Is(err, ErrPrinterDetectionPartial) {
		t.Fatalf("expected ErrPrinterDetectionPartial, got %v", err)
	}
	if printers != nil {
		t.Fatalf("expected nil slice, got %v", printers)
	}
}

func TestResolveConnectionPath_Empty(t *testing.T) {
	printers := []PrinterInfo{{ID: "cups:laser", ConnectionPath: "/dev/usb/lp0"}}
	path, err := ResolveConnectionPath("", printers)
	if err != nil {
		t.Fatalf("expected nil error for empty printerID, got %v", err)
	}
	if path != "" {
		t.Fatalf("expected empty path for non-print job, got %q", path)
	}
}

func TestResolveConnectionPath_Found(t *testing.T) {
	printers := []PrinterInfo{
		{ID: "usb:2C99", ConnectionPath: "/dev/ttyACM0"},
		{ID: "cups:laser", ConnectionPath: "/dev/usb/lp0"},
	}
	path, err := ResolveConnectionPath("cups:laser", printers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/dev/usb/lp0" {
		t.Fatalf("expected /dev/usb/lp0, got %q", path)
	}
}

func TestResolveConnectionPath_NotFound(t *testing.T) {
	printers := []PrinterInfo{
		{ID: "cups:laser", ConnectionPath: "/dev/usb/lp0"},
	}
	_, err := ResolveConnectionPath("usb:2C99", printers)
	if err == nil {
		t.Fatal("expected error for unknown printer ID, got nil")
	}
}
