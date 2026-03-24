package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func resetInit() {
	initOnce = sync.Once{}
}

func TestNew(t *testing.T) {
	resetInit()
	var buf bytes.Buffer
	Init("text", "debug", &buf)

	logger := New("scheduler")
	logger.Info("workload dispatched", "workload_id", "wl-123", "node", "did:key:abc")

	output := buf.String()
	if !strings.Contains(output, "subsystem=scheduler") {
		t.Errorf("expected subsystem=scheduler in output, got: %s", output)
	}
	if !strings.Contains(output, "workload dispatched") {
		t.Errorf("expected message in output, got: %s", output)
	}
	if !strings.Contains(output, "workload_id=wl-123") {
		t.Errorf("expected workload_id in output, got: %s", output)
	}
}

func TestLogLevels(t *testing.T) {
	resetInit()
	var buf bytes.Buffer
	Init("text", "warn", &buf)

	logger := New("test")
	logger.Info("should be suppressed")
	logger.Warn("should appear")

	output := buf.String()
	if strings.Contains(output, "should be suppressed") {
		t.Errorf("INFO message should be suppressed at WARN level")
	}
	if !strings.Contains(output, "should appear") {
		t.Errorf("WARN message should appear at WARN level")
	}
}

func TestJSONFormat(t *testing.T) {
	resetInit()
	var buf bytes.Buffer
	Init("json", "info", &buf)

	logger := New("payment")
	logger.Info("settlement complete", "amount_sats", 5000)

	output := buf.String()
	if !strings.Contains(output, `"subsystem":"payment"`) {
		t.Errorf("expected JSON subsystem field, got: %s", output)
	}
	if !strings.Contains(output, `"amount_sats":5000`) {
		t.Errorf("expected JSON amount_sats field, got: %s", output)
	}
}

func TestWithRequestID(t *testing.T) {
	resetInit()
	var buf bytes.Buffer
	Init("text", "info", &buf)

	logger := New("httpapi").WithRequestID("req-abc-123")
	logger.Info("request handled")

	output := buf.String()
	if !strings.Contains(output, "request_id=req-abc-123") {
		t.Errorf("expected request_id in output, got: %s", output)
	}
}

func TestContextRequestID(t *testing.T) {
	resetInit()
	var buf bytes.Buffer
	Init("text", "info", &buf)

	ctx := ContextWithRequestID(context.Background(), "ctx-req-456")
	logger := New("auth").WithContext(ctx)
	logger.Info("auth challenge")

	output := buf.String()
	if !strings.Contains(output, "request_id=ctx-req-456") {
		t.Errorf("expected request_id from context, got: %s", output)
	}
}

func TestRequestIDFromContext(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "rid-789")
	got := RequestIDFromContext(ctx)
	if got != "rid-789" {
		t.Errorf("expected rid-789, got %s", got)
	}

	// Empty context
	got = RequestIDFromContext(context.Background())
	if got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

func TestSetLevelRuntime(t *testing.T) {
	resetInit()
	var buf bytes.Buffer
	Init("text", "info", &buf)

	logger := New("test")

	// Debug should be suppressed at INFO level
	logger.Debug("hidden")
	if strings.Contains(buf.String(), "hidden") {
		t.Error("DEBUG should be suppressed at INFO level")
	}

	// Lower to debug
	SetLevel("debug")
	logger.Debug("visible now")
	if !strings.Contains(buf.String(), "visible now") {
		t.Error("DEBUG should appear after SetLevel(debug)")
	}
}

func TestParseLegacyLog(t *testing.T) {
	tests := []struct {
		input     string
		subsystem string
		level     slog.Level
		msg       string
	}{
		{
			input:     "[app] server started on :8080",
			subsystem: "app",
			level:     slog.LevelInfo,
			msg:       "server started on :8080",
		},
		{
			input:     "[WARN] TLS not configured",
			subsystem: "",
			level:     slog.LevelWarn,
			msg:       "TLS not configured",
		},
		{
			input:     "[WARN] [radius] secret missing",
			subsystem: "radius",
			level:     slog.LevelWarn,
			msg:       "secret missing",
		},
		{
			input:     "plain message without prefix",
			subsystem: "",
			level:     slog.LevelInfo,
			msg:       "plain message without prefix",
		},
	}

	for _, tt := range tests {
		subsystem, level, msg := parseLegacyLog(tt.input)
		if subsystem != tt.subsystem {
			t.Errorf("parseLegacyLog(%q): subsystem = %q, want %q", tt.input, subsystem, tt.subsystem)
		}
		if level != tt.level {
			t.Errorf("parseLegacyLog(%q): level = %v, want %v", tt.input, level, tt.level)
		}
		if msg != tt.msg {
			t.Errorf("parseLegacyLog(%q): msg = %q, want %q", tt.input, msg, tt.msg)
		}
	}
}

func TestSlogBridgeWrite(t *testing.T) {
	resetInit()
	var buf bytes.Buffer
	Init("text", "info", &buf)

	// Simulate what log.Printf("[app] starting") would produce
	bridge := &slogBridge{}
	bridge.Write([]byte("[app] starting services\n"))

	output := buf.String()
	if !strings.Contains(output, "starting services") {
		t.Errorf("bridge should forward message, got: %s", output)
	}
	if !strings.Contains(output, "subsystem=app") {
		t.Errorf("bridge should extract subsystem, got: %s", output)
	}
}
