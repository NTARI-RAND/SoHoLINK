package agent

import "testing"

func TestHasChanged_PrinterFieldChange(t *testing.T) {
	a := HardwareProfile{
		CPUCores: 4, RAMMB: 8192, StorageGB: 256,
		Platform: "linux", Arch: "amd64",
		Printers: []PrinterInfo{
			{ID: "cups:laser", Kind: PrinterKindTraditional, Name: "laser"},
		},
	}
	b := a
	b.Printers = []PrinterInfo{
		{ID: "cups:laser", Kind: PrinterKindTraditional, Name: "laser"},
		{ID: "usb:2C99", Kind: PrinterKind3D, Name: "Prusa"},
	}
	if !HasChanged(a, b) {
		t.Fatal("expected HasChanged true when printer list grows")
	}
}

func TestHasChanged_NoChangeWithReorderedPrinters(t *testing.T) {
	a := HardwareProfile{
		CPUCores: 4, RAMMB: 8192, StorageGB: 256,
		Platform: "linux", Arch: "amd64",
		Printers: []PrinterInfo{
			{ID: "cups:laser", Kind: PrinterKindTraditional, Name: "laser"},
			{ID: "usb:2C99", Kind: PrinterKind3D, Name: "Prusa"},
		},
	}
	b := HardwareProfile{
		CPUCores: 4, RAMMB: 8192, StorageGB: 256,
		Platform: "linux", Arch: "amd64",
		Printers: []PrinterInfo{
			{ID: "usb:2C99", Kind: PrinterKind3D, Name: "Prusa"},
			{ID: "cups:laser", Kind: PrinterKindTraditional, Name: "laser"},
		},
	}
	if HasChanged(a, b) {
		t.Fatal("expected HasChanged false when printers differ only in order")
	}
}

func TestHasChanged_CPUCoreChange(t *testing.T) {
	a := HardwareProfile{CPUCores: 4, Platform: "linux"}
	b := HardwareProfile{CPUCores: 8, Platform: "linux"}
	if !HasChanged(a, b) {
		t.Fatal("expected HasChanged true when CPU cores change")
	}
}

func TestHasChanged_Identical(t *testing.T) {
	a := HardwareProfile{
		CPUCores: 4, RAMMB: 8192, StorageGB: 256,
		Platform: "linux", Arch: "amd64",
	}
	b := a
	if HasChanged(a, b) {
		t.Fatal("expected HasChanged false for identical profiles")
	}
}

func TestPrintersEqual_BothEmpty(t *testing.T) {
	if !printersEqual(nil, nil) {
		t.Fatal("expected nil == nil")
	}
	if !printersEqual([]PrinterInfo{}, []PrinterInfo{}) {
		t.Fatal("expected empty == empty")
	}
}

func TestPrintersEqual_DifferentLengths(t *testing.T) {
	a := []PrinterInfo{{ID: "a"}}
	b := []PrinterInfo{{ID: "a"}, {ID: "b"}}
	if printersEqual(a, b) {
		t.Fatal("expected length difference to count as not equal")
	}
}

func TestPrintersEqual_SameContentDifferentOrder(t *testing.T) {
	a := []PrinterInfo{{ID: "a", Name: "alpha"}, {ID: "b", Name: "beta"}}
	b := []PrinterInfo{{ID: "b", Name: "beta"}, {ID: "a", Name: "alpha"}}
	if !printersEqual(a, b) {
		t.Fatal("expected reordered slices to be equal")
	}
}
