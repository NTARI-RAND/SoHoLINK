package portal

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// repoRoot returns the absolute path to the repository root by walking up from
// this file's location. Works regardless of where `go test` is invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file is .../internal/portal/testhelpers_test.go — go up two dirs
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// setupTestDB connects to DATABASE_URL and runs all migrations. It registers
// a cleanup function to close the pool when the test completes.
// Requires the integration build tag — callers are responsible for skipping
// when DATABASE_URL is not set.
func setupTestDB(t *testing.T) *store.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	db, err := store.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("store.Connect: %v", err)
	}
	t.Cleanup(func() { db.Pool.Close() })
	if err := store.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	// Clean slate: cascade from participants wipes nodes, jobs, disputes,
	// job_metering, node_heartbeat_events, and resource_profiles. resource_pricing
	// is migration-seeded static data and does not reference participants.
	if _, err := db.Pool.Exec(context.Background(),
		`TRUNCATE participants CASCADE`,
	); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return db
}

// newTestPortalServer creates a PortalServer wired with test Ed25519 keys,
// the provided DB, nil payment client, nil orchestrator, and the real
// web/templates directory from the repo root.
func newTestPortalServer(t *testing.T, db *store.DB) *PortalServer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	templatesDir := filepath.Join(repoRoot(t), "web", "templates")
	ps, err := New(db, ":0", priv, templatesDir, nil, "http://localhost", nil, ":0", "test-webhook-secret")
	if err != nil {
		t.Fatalf("portal.New: %v", err)
	}
	return ps
}

func newTestPortalServerWithOrch(t *testing.T, db *store.DB, orch jobSubmitter) *PortalServer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	templatesDir := filepath.Join(repoRoot(t), "web", "templates")
	ps, err := New(db, ":0", priv, templatesDir, nil, "http://localhost", orch, ":0", "test-webhook-secret")
	if err != nil {
		t.Fatalf("portal.New: %v", err)
	}
	return ps
}

// seedParticipant inserts a participant row with a bcrypt-hashed password
// (MinCost for test speed) and returns the participant UUID.
func seedParticipant(t *testing.T, db *store.DB, email, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	var id string
	err = db.Pool.QueryRow(context.Background(),
		`INSERT INTO participants (email, password_hash, display_name, soho_name)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		email, string(hash), email, "test-node",
	).Scan(&id)
	if err != nil {
		t.Fatalf("seedParticipant: %v", err)
	}
	return id
}

// seedNode inserts a node row owned by participantID and returns the node UUID.
func seedNode(t *testing.T, db *store.DB, participantID, status, class, country string) string {
	t.Helper()
	var id string
	err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO nodes (participant_id, hostname, status, node_class, country_code,
		  hardware_profile, uptime_pct)
		 VALUES ($1, $2, $3, $4, $5, '{"CPUCores":4,"RAMMB":8192}', 100.0) RETURNING id`,
		participantID, "test-host-"+status, status, class, country,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seedNode: %v", err)
	}
	return id
}

// authenticatedRequest creates an HTTP request with a valid session cookie
// for the given userID and email.
func authenticatedRequest(t *testing.T, sm *SessionManager, method, path, userID, email string) *http.Request {
	t.Helper()
	token, err := sm.CreateToken(SessionClaims{
		UserID: userID,
		Email:  email,
	})
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	req, err := http.NewRequest(method, path, nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	return req
}
