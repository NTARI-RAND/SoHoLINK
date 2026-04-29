package api

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
)

// handleGetAllowlist serves the signed allowlist file at allowlistPath.
// The file is served as-is; signature verification happens client-side using
// the agent's baked-in public key. This endpoint is intentionally not behind
// SPIFFE auth: fresh agents fetching for the first time do not yet have SVIDs.
//
// The file is re-read from disk on every request. Allowlist updates are rare
// and a single os.ReadFile is cheap; this avoids stale-cache bugs when the
// operator publishes a new signed allowlist by replacing the file.
func handleGetAllowlist(allowlistPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(allowlistPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				slog.Warn("allowlist file not found", "path", allowlistPath)
				http.Error(w, "allowlist not configured", http.StatusNotFound)
				return
			}
			slog.Error("allowlist read failed", "path", allowlistPath, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write(data); err != nil {
			slog.Warn("allowlist write to response failed", "error", err)
		}
	}
}
