package store

import (
	"context"
	"time"
)

// RunUptimeScorer runs in a goroutine and updates nodes.uptime_pct every
// interval based on heartbeat events over the past 7 days.
// Expected heartbeats = 7 * 24 * 60 * 2 (one per 30s).
func RunUptimeScorer(ctx context.Context, db *DB, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := scoreUptime(ctx, db); err != nil {
				// Non-fatal — log and continue on next tick.
				_ = err
			}
		}
	}
}

func scoreUptime(ctx context.Context, db *DB) error {
	rows, err := db.Pool.Query(ctx, `SELECT id FROM nodes`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var nodeIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		nodeIDs = append(nodeIDs, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, nodeID := range nodeIDs {
		var count int64
		err := db.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM node_heartbeat_events
			WHERE node_id = $1 AND recorded_at > NOW() - INTERVAL '7 days'`,
			nodeID,
		).Scan(&count)
		if err != nil {
			return err
		}

		pct := float64(count) / 20160.0 * 100.0
		if pct > 100.0 {
			pct = 100.0
		}

		_, err = db.Pool.Exec(ctx,
			`UPDATE nodes SET uptime_pct = $1 WHERE id = $2`,
			pct, nodeID,
		)
		if err != nil {
			return err
		}
	}

	return nil
}
