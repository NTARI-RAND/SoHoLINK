package accounting

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewCollector(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCollector(dir)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}
	defer c.Close()

	// Log file should exist
	today := time.Now().UTC().Format("2006-01-02")
	logFile := filepath.Join(dir, today+".jsonl")
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Errorf("log file should exist: %s", logFile)
	}
}

func TestRecordEvent(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCollector(dir)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	event := &AccountingEvent{
		EventType: "auth_success",
		UserDID:   "did:key:z6MkTest",
		Username:  "alice",
		Decision:  "ALLOW",
		LatencyUS: 1500,
	}

	if err := c.Record(event); err != nil {
		t.Fatalf("Record failed: %v", err)
	}
	c.Close()

	// Read back the event
	logFile := c.CurrentFile()
	f, err := os.Open(logFile)
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("expected at least one line in log")
	}

	var recorded AccountingEvent
	if err := json.Unmarshal(scanner.Bytes(), &recorded); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	if recorded.EventType != "auth_success" {
		t.Errorf("expected event_type 'auth_success', got '%s'", recorded.EventType)
	}
	if recorded.Username != "alice" {
		t.Errorf("expected username 'alice', got '%s'", recorded.Username)
	}
	if recorded.Timestamp.IsZero() {
		t.Error("timestamp should be set automatically")
	}
}

func TestRecordMultipleEvents(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCollector(dir)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	for i := 0; i < 10; i++ {
		event := &AccountingEvent{
			EventType: "auth_success",
			Username:  "user",
			Decision:  "ALLOW",
		}
		if err := c.Record(event); err != nil {
			t.Fatalf("Record %d failed: %v", i, err)
		}
	}
	c.Close()

	// Count lines
	logFile := c.CurrentFile()
	f, _ := os.Open(logFile)
	defer f.Close()

	lines := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines++
	}
	if lines != 10 {
		t.Errorf("expected 10 lines, got %d", lines)
	}
}

func TestEventCount(t *testing.T) {
	dir := t.TempDir()
	c, _ := NewCollector(dir)
	defer c.Close()

	if c.EventCount() != 0 {
		t.Error("initial event count should be 0")
	}

	c.Record(&AccountingEvent{EventType: "test"})
	c.Record(&AccountingEvent{EventType: "test"})

	if c.EventCount() != 2 {
		t.Errorf("expected event count 2, got %d", c.EventCount())
	}
}

func TestCompressOldLogs(t *testing.T) {
	dir := t.TempDir()

	// Create an "old" log file (10 days ago)
	oldDate := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	oldFile := filepath.Join(dir, oldDate+".jsonl")
	os.WriteFile(oldFile, []byte(`{"event":"old"}`+"\n"), 0640)

	// Create a "recent" log file (today)
	recentDate := time.Now().Format("2006-01-02")
	recentFile := filepath.Join(dir, recentDate+".jsonl")
	os.WriteFile(recentFile, []byte(`{"event":"recent"}`+"\n"), 0640)

	// Compress files older than 7 days
	compressed, err := CompressOldLogs(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("CompressOldLogs failed: %v", err)
	}
	if compressed != 1 {
		t.Errorf("expected 1 compressed file, got %d", compressed)
	}

	// Old file should be gone, .gz should exist
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("old .jsonl file should be removed after compression")
	}
	if _, err := os.Stat(oldFile + ".gz"); os.IsNotExist(err) {
		t.Error("compressed .gz file should exist")
	}

	// Recent file should still exist uncompressed
	if _, err := os.Stat(recentFile); os.IsNotExist(err) {
		t.Error("recent .jsonl file should not be compressed")
	}
}
