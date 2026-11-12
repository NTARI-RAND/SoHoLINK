# Rolling uptime scoring

Each scoring tick issues one PostgreSQL `UPDATE` for all nodes. A correlated
count uses the existing `idx_heartbeat_node_time (node_id, recorded_at DESC)`
index to read each node's recent range. It excludes timestamps at or before
`NOW() - INTERVAL '7 days'`; duplicate heartbeat events still count separately.
Nodes without recent events receive zero, and scores remain capped at 100.
No schema migration or heartbeat-retention policy is introduced.

The stored percentage is `COUNT(*)::numeric / 20160 * 100`, rounded by the
existing `NUMERIC(5,2)` column. This differs slightly from the old Go float
calculation: counts 2898, 5166, 6426, 10962 and 12222 now store 14.38, 25.63,
31.88, 54.38 and 60.63 respectively, 0.01 above the old floating-point result.
These values do not cross the 85% or 95% class thresholds. A count of 19152
is exactly 95.00%, not 94.97%.

`soholink_uptime_scorer_duration_seconds` observes each scoring attempt,
including failures, in seconds. The existing scorer still logs a failed
attempt and retries on the next tick. The metric adds no registration step or
new subsystem.

## Database verification

Use a dedicated database whose name contains `test`, as described in
[test-database.md](test-database.md). Never use the production `DATABASE_URL`.

```sh
go test -race ./internal/store -run 'TestScoreUptime|TestRunUptimeScorer' -count=1 -v
go test -race -tags integration -count=1 -p 1 ./...
```

`TestScoreUptimeExactPercentages` calls the scorer directly without ticker sleeps,
starting nodes at a sentinel score. It covers empty/stale histories, exact 95%,
the cap, duplicates and all five numeric half-rounding cases.
`TestScoreUptimeRecordsDurationForSuccessAndFailure` checks observations and
propagated cancellation. `TestScoreUptimeSkipsStaleHeartbeatHistory` traces the
actual executed statement and runs `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`
against eight fixture nodes, 480000 events aged 28 days and 256 recent events.
It checks one scoring statement and a bounded heartbeat-buffer count.

On the isolated PostgreSQL 16 review fixture, the broad aggregation touched
1825 heartbeat blocks; the indexed per-node count touched 33. Both plans were
executed, not estimated. These buffer counts describe that fixture only;
execution times and production performance depend on node count, history,
statistics and cache state. The test prints its complete plan for inspection.
