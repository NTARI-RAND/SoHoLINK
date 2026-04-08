package agent

import (
	"testing"
	"time"
)

func TestSignTelemetry_NonEmptyAndDeterministic(t *testing.T) {
	secret := []byte("test-secret")
	p := TelemetryPayload{
		NodeID:    "node-1",
		JobID:     "job-1",
		CPUPct:    42.5,
		RAMPct:    60.0,
		Timestamp: time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC),
	}

	signed1, err := SignTelemetry(p, secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signed1.Signature == "" {
		t.Fatal("expected non-empty signature")
	}

	signed2, err := SignTelemetry(p, secret)
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if signed1.Signature != signed2.Signature {
		t.Errorf("signatures differ for identical inputs: %q vs %q", signed1.Signature, signed2.Signature)
	}
}

func TestSignTelemetry_EmptyIDReturnsError(t *testing.T) {
	secret := []byte("test-secret")
	ts := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		label  string
		nodeID string
		jobID  string
	}{
		{"empty NodeID", "", "job-1"},
		{"empty JobID", "node-1", ""},
		{"both empty", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			_, err := SignTelemetry(TelemetryPayload{
				NodeID:    tc.nodeID,
				JobID:     tc.jobID,
				Timestamp: ts,
			}, secret)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestSignTelemetry_TamperedPayloadDifferentSignature(t *testing.T) {
	secret := []byte("test-secret")
	base := TelemetryPayload{
		NodeID:    "node-1",
		JobID:     "job-1",
		CPUPct:    42.5,
		RAMPct:    60.0,
		Timestamp: time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC),
	}

	original, err := SignTelemetry(base, secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tampered := base
	tampered.CPUPct = 99.9
	tamperedSigned, err := SignTelemetry(tampered, secret)
	if err != nil {
		t.Fatalf("unexpected error signing tampered payload: %v", err)
	}

	if original.Signature == tamperedSigned.Signature {
		t.Error("expected different signatures for different CPUPct, got identical")
	}
}
