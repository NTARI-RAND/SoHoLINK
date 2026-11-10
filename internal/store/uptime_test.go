package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func uptimeTestDB(t *testing.T) (*DB, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test (see docs/test-database.md)")
	}
	db, err := Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Pool.Close() })
	var name string
	if err := db.Pool.QueryRow(context.Background(), "SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(name, "test") {
		t.Fatalf("refusing to modify non-test database %q", name)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	var participant string
	if err := db.Pool.QueryRow(context.Background(), `INSERT INTO participants (email, password_hash, display_name) VALUES ($1, 'x', 'Uptime Test') RETURNING id`, "uptime-"+uuid.NewString()+"@example.test").Scan(&participant); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Pool.Exec(context.Background(), `DELETE FROM participants WHERE id = $1`, participant); err != nil {
			t.Error(err)
		}
	})
	return db, participant
}

func seedUptimeNode(t *testing.T, db *DB, participant string, count int, age time.Duration) string {
	t.Helper()
	var node string
	if err := db.Pool.QueryRow(context.Background(), `INSERT INTO nodes (participant_id, hostname, country_code, node_class, uptime_pct) VALUES ($1, $2, 'US', 'C', 17.00) RETURNING id`, participant, "uptime-"+uuid.NewString()).Scan(&node); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(context.Background(), `INSERT INTO node_heartbeat_events (node_id, recorded_at) SELECT $1, NOW() - $3::interval FROM generate_series(1, $2)`, node, count, fmt.Sprintf("%f seconds", age.Seconds())); err != nil {
		t.Fatal(err)
	}
	return node
}

func TestScoreUptimeExactPercentages(t *testing.T) {
	db, participant := uptimeTestDB(t)
	for _, tc := range []struct {
		name  string
		count int
		age   time.Duration
		want  string
	}{
		{"no heartbeats", 0, time.Hour, "0.00"},
		{"stale only", 12, 8 * 24 * time.Hour, "0.00"},
		{"19152 is exactly 95 percent", 19152, time.Hour, "95.00"},
		{"full window", 20160, time.Hour, "100.00"},
		{"above cap", 22000, time.Hour, "100.00"},
		{"numeric half 2898", 2898, time.Hour, "14.38"},
		{"numeric half 5166", 5166, time.Hour, "25.63"},
		{"numeric half 6426", 6426, time.Hour, "31.88"},
		{"numeric half 10962", 10962, time.Hour, "54.38"},
		{"numeric half 12222", 12222, time.Hour, "60.63"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := seedUptimeNode(t, db, participant, tc.count, tc.age)
			if err := scoreUptime(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			var got string
			if err := db.Pool.QueryRow(context.Background(), `SELECT uptime_pct::text FROM nodes WHERE id=$1`, node).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("uptime_pct = %s; want %s", got, tc.want)
			}
		})
	}
}

func TestRunUptimeScorerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RunUptimeScorer(ctx, &DB{}, time.Hour); err != nil {
		t.Fatal(err)
	}
}
