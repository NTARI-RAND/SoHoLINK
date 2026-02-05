package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/accounting"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/config"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View accounting logs",
	Long:  `Display accounting events from the append-only JSONL log files.`,
	RunE:  runLogs,
}

var (
	logsFollow   bool
	logsType     string
	logsUser     string
	logsDate     string
	logsLastN    int
)

func init() {
	rootCmd.AddCommand(logsCmd)
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "follow log output (like tail -f)")
	logsCmd.Flags().StringVar(&logsType, "type", "", "filter by event type (auth_success, auth_failure, etc.)")
	logsCmd.Flags().StringVar(&logsUser, "user", "", "filter by username")
	logsCmd.Flags().StringVar(&logsDate, "date", "", "show logs for specific date (YYYY-MM-DD)")
	logsCmd.Flags().IntVarP(&logsLastN, "last", "n", 50, "show last N events")
}

func runLogs(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if dataDir != "" {
		cfg.Storage.BasePath = dataDir
	}

	acctDir := cfg.AccountingDir()

	// Determine which log file to read
	date := logsDate
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}

	logFile := filepath.Join(acctDir, date+".jsonl")
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		fmt.Printf("No log file for %s\n", date)
		fmt.Printf("Looking in: %s\n", acctDir)
		// List available log files
		entries, _ := os.ReadDir(acctDir)
		if len(entries) > 0 {
			fmt.Println("\nAvailable log files:")
			for _, e := range entries {
				fmt.Printf("  %s\n", e.Name())
			}
		}
		return nil
	}

	if logsFollow {
		return followLog(logFile)
	}

	return showLog(logFile)
}

func showLog(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open log: %w", err)
	}
	defer f.Close()

	// Read all matching events
	var events []accounting.AccountingEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		var event accounting.AccountingEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue // Skip malformed lines
		}

		// Apply filters
		if logsType != "" && event.EventType != logsType {
			continue
		}
		if logsUser != "" && event.Username != logsUser {
			continue
		}

		events = append(events, event)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading log: %w", err)
	}

	// Show last N events
	start := 0
	if len(events) > logsLastN {
		start = len(events) - logsLastN
	}

	for i := start; i < len(events); i++ {
		printEvent(&events[i])
	}

	fmt.Printf("\nShowing %d of %d events from %s\n", len(events)-start, len(events), filepath.Base(path))

	return nil
}

func followLog(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open log: %w", err)
	}
	defer f.Close()

	// Seek to end
	f.Seek(0, io.SeekEnd)

	fmt.Printf("Following %s (Ctrl+C to stop)...\n\n", filepath.Base(path))

	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var event accounting.AccountingEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		// Apply filters
		if logsType != "" && event.EventType != logsType {
			continue
		}
		if logsUser != "" && event.Username != logsUser {
			continue
		}

		printEvent(&event)
	}
}

func printEvent(e *accounting.AccountingEvent) {
	ts := e.Timestamp.Format("15:04:05")

	// Color-code decision
	decision := e.Decision
	if decision == "" {
		decision = e.EventType
	}

	userInfo := e.Username
	if userInfo == "" && e.UserDID != "" {
		userInfo = e.UserDID
		if len(userInfo) > 20 {
			userInfo = userInfo[:20] + "..."
		}
	}

	latency := ""
	if e.LatencyUS > 0 {
		if e.LatencyUS < 1000 {
			latency = fmt.Sprintf(" (%dus)", e.LatencyUS)
		} else {
			latency = fmt.Sprintf(" (%.1fms)", float64(e.LatencyUS)/1000.0)
		}
	}

	fmt.Printf("[%s] %-14s %-12s %-6s%s", ts, e.EventType, userInfo, decision, latency)
	if e.Reason != "" {
		fmt.Printf(" reason=%s", e.Reason)
	}
	if e.ClientIP != "" {
		fmt.Printf(" from=%s", e.ClientIP)
	}
	fmt.Println()
}
