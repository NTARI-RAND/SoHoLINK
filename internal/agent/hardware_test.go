package agent

import (
	"context"
	"testing"
)

func TestDetect(t *testing.T) {
	p, err := Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect: unexpected error: %v", err)
	}
	if p.CPUCores <= 0 {
		t.Errorf("CPUCores: got %d, want > 0", p.CPUCores)
	}
	if p.RAMMB <= 0 {
		t.Errorf("RAMMB: got %d, want > 0", p.RAMMB)
	}
	if p.StorageGB <= 0 {
		t.Errorf("StorageGB: got %d, want > 0", p.StorageGB)
	}
	if p.Platform == "" {
		t.Error("Platform: got empty string")
	}
	if p.Arch == "" {
		t.Error("Arch: got empty string")
	}
}
