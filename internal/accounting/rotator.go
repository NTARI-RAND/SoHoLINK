package accounting

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CompressOldLogs finds .jsonl files older than maxAge in the given directory
// and compresses them to .jsonl.gz, removing the original.
func CompressOldLogs(dir string, maxAge time.Duration) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("failed to read directory: %w", err)
	}

	compressed := 0
	cutoff := time.Now().Add(-maxAge)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		// Only process .jsonl files (not already compressed)
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}

		// Parse date from filename (YYYY-MM-DD.jsonl)
		dateStr := strings.TrimSuffix(name, ".jsonl")
		fileDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue // Skip files with non-date names
		}

		// Only compress files older than maxAge
		if fileDate.After(cutoff) {
			continue
		}

		srcPath := filepath.Join(dir, name)
		dstPath := srcPath + ".gz"

		// Skip if already compressed
		if _, err := os.Stat(dstPath); err == nil {
			continue
		}

		if err := compressFile(srcPath, dstPath); err != nil {
			return compressed, fmt.Errorf("failed to compress %s: %w", name, err)
		}

		// Remove original after successful compression
		if err := os.Remove(srcPath); err != nil {
			return compressed, fmt.Errorf("failed to remove original %s: %w", name, err)
		}

		compressed++
	}

	return compressed, nil
}

// compressFile reads src, gzip-compresses it, and writes to dst.
func compressFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	gz := gzip.NewWriter(dstFile)
	gz.Name = filepath.Base(src)
	gz.ModTime = time.Now()

	if _, err := io.Copy(gz, srcFile); err != nil {
		gz.Close()
		os.Remove(dst)
		return err
	}

	if err := gz.Close(); err != nil {
		os.Remove(dst)
		return err
	}

	return dstFile.Sync()
}
