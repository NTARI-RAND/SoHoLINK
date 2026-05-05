package agent

import (
	"testing"
)

func TestPrinterHash_Empty(t *testing.T) {
	if got := PrinterHash(nil); got != "" {
		t.Errorf("expected empty string for nil slice, got %q", got)
	}
	if got := PrinterHash([]PrinterInfo{}); got != "" {
		t.Errorf("expected empty string for empty slice, got %q", got)
	}
}

func TestPrinterHash_OrderIndependent(t *testing.T) {
	a := []PrinterInfo{{ID: "printer-1"}, {ID: "printer-2"}}
	b := []PrinterInfo{{ID: "printer-2"}, {ID: "printer-1"}}
	if PrinterHash(a) != PrinterHash(b) {
		t.Errorf("expected same hash regardless of input order")
	}
}

func TestPrinterHash_DifferentIDs(t *testing.T) {
	a := []PrinterInfo{{ID: "printer-A"}}
	b := []PrinterInfo{{ID: "printer-B"}}
	if PrinterHash(a) == PrinterHash(b) {
		t.Errorf("expected different hashes for different printer IDs")
	}
}
