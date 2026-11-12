package store

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type uptimeQueryCapture struct {
	mu         sync.Mutex
	statements []string
}

func (c *uptimeQueryCapture) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statements = append(c.statements, data.SQL)
	return ctx
}
func (*uptimeQueryCapture) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

type uptimePlan struct {
	NodeType         string       `json:"Node Type"`
	RelationName     string       `json:"Relation Name"`
	SharedHitBlocks  int          `json:"Shared Hit Blocks"`
	SharedReadBlocks int          `json:"Shared Read Blocks"`
	Plans            []uptimePlan `json:"Plans"`
}

func heartbeatScanBlocks(plan uptimePlan) int {
	blocks := 0
	if plan.RelationName == "node_heartbeat_events" {
		blocks += plan.SharedHitBlocks + plan.SharedReadBlocks
	}
	for _, child := range plan.Plans {
		blocks += heartbeatScanBlocks(child)
	}
	return blocks
}

func TestScoreUptimeSkipsStaleHeartbeatHistory(t *testing.T) {
	db, participant := uptimeTestDB(t)
	capture := &uptimeQueryCapture{}
	config := db.Pool.Config()
	config.ConnConfig.Tracer = capture
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	db.Pool.Close()
	db.Pool = pool
	for i := 0; i < 8; i++ {
		node := seedUptimeNode(t, db, participant, 60000, 28*24*time.Hour)
		if _, err := db.Pool.Exec(context.Background(), `INSERT INTO node_heartbeat_events (node_id, recorded_at) SELECT $1, NOW() - INTERVAL '1 hour' FROM generate_series(1, 32)`, node); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool.Exec(context.Background(), "ANALYZE node_heartbeat_events"); err != nil {
		t.Fatal(err)
	}
	capture.mu.Lock()
	capture.statements = nil
	capture.mu.Unlock()
	if err := scoreUptime(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	capture.mu.Lock()
	statements := append([]string(nil), capture.statements...)
	capture.mu.Unlock()
	if len(statements) != 1 {
		t.Fatalf("scorer executed %d statements; want one", len(statements))
	}
	if !strings.Contains(strings.ToUpper(statements[0]), "UPDATE") {
		t.Fatal("scorer did not update scores")
	}
	var raw []byte
	if err := db.Pool.QueryRow(context.Background(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+statements[0]).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plans []struct {
		Plan          uptimePlan
		ExecutionTime float64 `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil {
		t.Fatal(err)
	}
	blocks := heartbeatScanBlocks(plans[0].Plan)
	t.Logf("480000 stale + 256 recent heartbeats: scan blocks=%d, execution=%.3f ms; plan=%s", blocks, plans[0].ExecutionTime, raw)
	if blocks > 256 {
		t.Errorf("heartbeat scans touched %d blocks; want <=256 for bounded recent ranges", blocks)
	}
}
