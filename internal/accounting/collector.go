package accounting

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AccountingEvent represents a single auditable event in the system.
type AccountingEvent struct {
	Timestamp    time.Time         `json:"timestamp"`
	EventType    string            `json:"event_type"`
	UserDID      string            `json:"user_did,omitempty"`
	Username     string            `json:"username,omitempty"`
	NASAddress   string            `json:"nas_address,omitempty"`
	NASIdentifier string           `json:"nas_identifier,omitempty"`
	SessionID    string            `json:"session_id,omitempty"`
	Resource     string            `json:"resource,omitempty"`
	PolicyResult string            `json:"policy_result,omitempty"`
	Decision     string            `json:"decision,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	LatencyUS    int64             `json:"latency_us,omitempty"`
	ClientIP     string            `json:"client_ip,omitempty"`
	PolicyHash   string            `json:"policy_hash,omitempty"`
	Attributes   map[string]string `json:"attributes,omitempty"`
}

// Collector writes accounting events to append-only JSONL files.
type Collector struct {
	dir         string
	currentDate string
	file        *os.File
	mu          sync.Mutex
	eventCount  int64
	syncEvery   int // fsync after this many events
}

// NewCollector creates a new accounting collector writing to the given directory.
func NewCollector(dir string) (*Collector, error) {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create accounting directory: %w", err)
	}

	c := &Collector{
		dir:       dir,
		syncEvery: 100,
	}

	// Open current day's log file
	if err := c.openFile(); err != nil {
		return nil, err
	}

	return c, nil
}

// Record writes an accounting event to the current log file.
// This is safe for concurrent use.
func (c *Collector) Record(event *AccountingEvent) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we need to rotate
	today := time.Now().UTC().Format("2006-01-02")
	if today != c.currentDate {
		if err := c.rotate(today); err != nil {
			return fmt.Errorf("failed to rotate log: %w", err)
		}
	}

	// Write event as JSONL (one JSON object per line)
	if _, err := c.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}

	c.eventCount++

	// Periodic fsync for durability
	if c.eventCount%int64(c.syncEvery) == 0 {
		if err := c.file.Sync(); err != nil {
			return fmt.Errorf("failed to fsync: %w", err)
		}
	}

	return nil
}

// openFile opens the current day's log file in append mode.
func (c *Collector) openFile() error {
	c.currentDate = time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(c.dir, c.currentDate+".jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0640)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", path, err)
	}

	c.file = f
	return nil
}

// rotate closes the current file and opens a new one for the given date.
func (c *Collector) rotate(newDate string) error {
	if c.file != nil {
		c.file.Sync()
		c.file.Close()
	}

	c.currentDate = newDate
	path := filepath.Join(c.dir, c.currentDate+".jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0640)
	if err != nil {
		return fmt.Errorf("failed to open rotated log file %s: %w", path, err)
	}

	c.file = f
	c.eventCount = 0
	return nil
}

// Close flushes and closes the current log file.
func (c *Collector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.file != nil {
		c.file.Sync()
		return c.file.Close()
	}
	return nil
}

// CurrentFile returns the path to the current log file.
func (c *Collector) CurrentFile() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return filepath.Join(c.dir, c.currentDate+".jsonl")
}

// Dir returns the accounting directory.
func (c *Collector) Dir() string {
	return c.dir
}

// EventCount returns the number of events written in the current file.
func (c *Collector) EventCount() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.eventCount
}
