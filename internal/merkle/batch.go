package merkle

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// BatchRecord represents the metadata of a Merkle batch.
type BatchRecord struct {
	Timestamp  time.Time `json:"timestamp"`
	SourceFile string    `json:"source_file"`
	RootHash   string    `json:"root_hash"`
	LeafCount  int       `json:"leaf_count"`
	TreeHeight int       `json:"tree_height"`
}

// Batcher periodically builds Merkle trees from accounting log files.
type Batcher struct {
	accountingDir string
	merkleDir     string
	interval      time.Duration
}

// NewBatcher creates a new Merkle batcher.
func NewBatcher(accountingDir, merkleDir string, interval time.Duration) *Batcher {
	os.MkdirAll(merkleDir, 0750)
	return &Batcher{
		accountingDir: accountingDir,
		merkleDir:     merkleDir,
		interval:      interval,
	}
}

// Start begins the periodic batching loop. It blocks until the context is cancelled.
func (b *Batcher) Start(ctx context.Context) {
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := b.BuildBatch(); err != nil {
				log.Printf("[merkle] batch failed: %v", err)
			}
		}
	}
}

// BuildBatch builds a Merkle tree from the current day's accounting log.
func (b *Batcher) BuildBatch() error {
	today := time.Now().UTC().Format("2006-01-02")
	logFile := filepath.Join(b.accountingDir, today+".jsonl")

	// Check if log file exists
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		return nil // No log file yet today
	}

	// Read all lines from the log file
	leaves, err := readLogLines(logFile)
	if err != nil {
		return fmt.Errorf("failed to read log file: %w", err)
	}

	if len(leaves) == 0 {
		return nil // Empty log
	}

	// Build Merkle tree
	tree, err := NewTree(leaves)
	if err != nil {
		return fmt.Errorf("failed to build Merkle tree: %w", err)
	}

	// Create batch record
	now := time.Now().UTC()
	batch := BatchRecord{
		Timestamp:  now,
		SourceFile: today + ".jsonl",
		RootHash:   hex.EncodeToString(tree.Root()),
		LeafCount:  tree.LeafCount(),
		TreeHeight: tree.Height(),
	}

	// Write batch record
	batchFile := filepath.Join(b.merkleDir, fmt.Sprintf("%s.batch.json", now.Format("2006-01-02T15")))
	data, err := json.MarshalIndent(batch, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal batch record: %w", err)
	}

	if err := os.WriteFile(batchFile, data, 0640); err != nil {
		return fmt.Errorf("failed to write batch record: %w", err)
	}

	log.Printf("[merkle] batch created: %s (leaves=%d, root=%s)", batchFile, batch.LeafCount, batch.RootHash[:16]+"...")

	return nil
}

// LatestBatch returns the most recent batch record, or nil if none exist.
func (b *Batcher) LatestBatch() (*BatchRecord, error) {
	pattern := filepath.Join(b.merkleDir, "*.batch.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to glob batch files: %w", err)
	}

	if len(files) == 0 {
		return nil, nil
	}

	// Files are sorted lexicographically; the last one is the most recent
	latestFile := files[len(files)-1]
	data, err := os.ReadFile(latestFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read batch file: %w", err)
	}

	var batch BatchRecord
	if err := json.Unmarshal(data, &batch); err != nil {
		return nil, fmt.Errorf("failed to parse batch record: %w", err)
	}

	return &batch, nil
}

// readLogLines reads a JSONL file and returns each line as a byte slice.
func readLogLines(path string) ([][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines [][]byte
	scanner := bufio.NewScanner(f)
	// Increase buffer size for potentially long lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// Make a copy since scanner reuses the buffer
		lineCopy := make([]byte, len(line))
		copy(lineCopy, line)
		lines = append(lines, lineCopy)
	}

	return lines, scanner.Err()
}
