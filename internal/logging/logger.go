// Package logging provides structured logging for SoHoLINK.
//
// Uses Go's built-in log/slog for structured, leveled, machine-parseable output.
// All subsystems should use this package instead of log.Printf.
//
// Usage:
//
//	logger := logging.New("orchestration")
//	logger.Info("workload scheduled", "workload_id", wlID, "node", nodeDID)
//	logger.Error("placement failed", "error", err, "workload_id", wlID)
//	logger.Warn("GPU thermal threshold", "temp_c", 82, "node", nodeDID)
//	logger.Debug("candidate filtering", "candidates", len(nodes))
package logging

import (
	"context"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// Level aliases for convenience.
const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// ctxKey is the context key type for request-scoped values.
type ctxKey string

const requestIDKey ctxKey = "request_id"

// globalLevel controls the minimum log level for all loggers.
var globalLevel = &slog.LevelVar{}

// initOnce ensures the default logger is initialized exactly once.
var initOnce sync.Once

// Logger wraps slog.Logger with a subsystem name baked in.
type Logger struct {
	*slog.Logger
	subsystem string
}

// Init configures the global logging system.
// Call once at startup (from main or app.New).
//
//	format: "json" for production, "text" for development
//	level:  "debug", "info", "warn", "error"
//	output: where to write (os.Stdout, os.Stderr, or a file)
func Init(format, level string, output io.Writer) {
	initOnce.Do(func() {
		setLevel(level)

		if output == nil {
			output = os.Stderr
		}

		var handler slog.Handler
		opts := &slog.HandlerOptions{
			Level:     globalLevel,
			AddSource: globalLevel.Level() == slog.LevelDebug,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				// Use shorter time format for text output
				if a.Key == slog.TimeKey {
					a.Value = slog.StringValue(a.Value.Time().Format(time.RFC3339))
				}
				return a
			},
		}

		switch strings.ToLower(format) {
		case "json":
			handler = slog.NewJSONHandler(output, opts)
		default:
			handler = slog.NewTextHandler(output, opts)
		}

		slog.SetDefault(slog.New(handler))

		// Bridge stdlib log.Printf → slog so legacy calls get structured output too.
		// This means existing log.Printf("[app] message") calls will flow through slog
		// as INFO-level messages until they're individually migrated.
		log.SetOutput(&slogBridge{})
		log.SetFlags(0) // slog handles timestamps
	})
}

// SetLevel changes the global log level at runtime.
// Safe to call from any goroutine.
func SetLevel(level string) {
	setLevel(level)
}

func setLevel(level string) {
	switch strings.ToLower(level) {
	case "debug":
		globalLevel.Set(slog.LevelDebug)
	case "warn", "warning":
		globalLevel.Set(slog.LevelWarn)
	case "error":
		globalLevel.Set(slog.LevelError)
	default:
		globalLevel.Set(slog.LevelInfo)
	}
}

// New creates a Logger for a specific subsystem.
// The subsystem name appears in every log entry, making it easy to filter.
//
//	logger := logging.New("scheduler")
//	logger.Info("job dispatched", "job_id", "abc123")
//	// Output: time=... level=INFO msg="job dispatched" subsystem=scheduler job_id=abc123
func New(subsystem string) *Logger {
	// Ensure default initialization if Init() wasn't called
	initOnce.Do(func() {
		Init("text", "info", os.Stderr)
	})

	l := slog.Default().With("subsystem", subsystem)
	return &Logger{Logger: l, subsystem: subsystem}
}

// WithRequestID returns a new Logger that includes the request ID in every entry.
func (l *Logger) WithRequestID(requestID string) *Logger {
	return &Logger{
		Logger:    l.Logger.With("request_id", requestID),
		subsystem: l.subsystem,
	}
}

// WithContext extracts the request ID from context (if present) and returns
// a Logger that includes it.
func (l *Logger) WithContext(ctx context.Context) *Logger {
	if rid, ok := ctx.Value(requestIDKey).(string); ok && rid != "" {
		return l.WithRequestID(rid)
	}
	return l
}

// ContextWithRequestID returns a new context with the request ID attached.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromContext extracts the request ID from context.
func RequestIDFromContext(ctx context.Context) string {
	if rid, ok := ctx.Value(requestIDKey).(string); ok {
		return rid
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// slogBridge bridges stdlib log.Printf → slog
// ─────────────────────────────────────────────────────────────────────────────

// slogBridge implements io.Writer to capture log.Printf output and route it
// through slog. This provides a migration path: existing log.Printf calls
// get structured output immediately, and can be migrated to slog.Info/etc.
// individually over time.
type slogBridge struct{}

func (b *slogBridge) Write(p []byte) (n int, err error) {
	msg := strings.TrimSpace(string(p))
	if msg == "" {
		return len(p), nil
	}

	// Parse the existing [prefix] pattern to extract subsystem and level.
	subsystem, level, cleanMsg := parseLegacyLog(msg)

	logger := slog.Default()
	if subsystem != "" {
		logger = logger.With("subsystem", subsystem)
	}

	switch level {
	case slog.LevelWarn:
		logger.Warn(cleanMsg)
	case slog.LevelError:
		logger.Error(cleanMsg)
	case slog.LevelDebug:
		logger.Debug(cleanMsg)
	default:
		logger.Info(cleanMsg)
	}

	return len(p), nil
}

// parseLegacyLog extracts subsystem and level from "[prefix] message" format.
func parseLegacyLog(msg string) (subsystem string, level slog.Level, cleanMsg string) {
	level = slog.LevelInfo
	cleanMsg = msg

	// Look for [WARN], [ERROR], [DEBUG] markers first
	if strings.HasPrefix(msg, "[WARN]") {
		level = slog.LevelWarn
		cleanMsg = strings.TrimSpace(msg[6:])
	} else if strings.HasPrefix(msg, "[ERROR]") {
		level = slog.LevelError
		cleanMsg = strings.TrimSpace(msg[7:])
	} else if strings.HasPrefix(msg, "[DEBUG]") {
		level = slog.LevelDebug
		cleanMsg = strings.TrimSpace(msg[7:])
	}

	// Extract [subsystem] prefix
	if len(cleanMsg) > 0 && cleanMsg[0] == '[' {
		end := strings.Index(cleanMsg, "]")
		if end > 0 && end < 30 { // reasonable prefix length
			subsystem = cleanMsg[1:end]
			cleanMsg = strings.TrimSpace(cleanMsg[end+1:])
		}
	}

	return subsystem, level, cleanMsg
}
