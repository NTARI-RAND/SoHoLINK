//go:build integration

package store_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/store"
)

// seedOperator inserts an operator row and removes it, with its nodes, after
// the test. Operator-owned nodes reference operators (ON DELETE RESTRICT), not
// participants, so connectTestDB's TRUNCATE does not reach them.
func seedOperator(t *testing.T, db *store.DB, id string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx,
		`INSERT INTO operators (id, name, email) VALUES ($1, $1, $1 || '@test.internal')`, id,
	); err != nil {
		t.Fatalf("seed operator %s: %v", id, err)
	}
	t.Cleanup(func() {
		db.Pool.Exec(context.Background(), `DELETE FROM nodes WHERE operator_id = $1`, id) //nolint:errcheck
		db.Pool.Exec(context.Background(), `DELETE FROM operators WHERE id = $1`, id)      //nolint:errcheck
	})
}

func newKey(t *testing.T) ed25519.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func seedParticipantNode(t *testing.T, db *store.DB) string {
	t.Helper()
	ctx := context.Background()
	var pid, nid string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO participants (email, display_name) VALUES ('opnodes-person@test.com', 'Person') RETURNING id`,
	).Scan(&pid); err != nil {
		t.Fatalf("seed participant: %v", err)
	}
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO nodes (participant_id, node_class, hostname, country_code)
		 VALUES ($1, 'A', 'person-host', 'US') RETURNING id`, pid,
	).Scan(&nid); err != nil {
		t.Fatalf("seed participant node: %v", err)
	}
	return nid
}

// The database itself enforces exactly one owner, and keeps every
// person-derived column empty on an operator-owned node.
func TestMigration030_ExactlyOneOwner(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	seedOperator(t, db, "op-owner-rule")
	var pid string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO participants (email, display_name) VALUES ('owner-rule@test.com', 'P') RETURNING id`,
	).Scan(&pid); err != nil {
		t.Fatal(err)
	}

	refused := map[string]string{
		"no owner": `INSERT INTO nodes (node_class) VALUES ('C')`,
		"two owners": `INSERT INTO nodes (participant_id, operator_id, node_class, hostname, country_code)
		               VALUES ('` + pid + `', 'op-owner-rule', 'C', 'h', 'US')`,
		"participant node without hostname": `INSERT INTO nodes (participant_id, node_class, country_code)
		               VALUES ('` + pid + `', 'C', 'US')`,
		"operator node with a hostname":              `INSERT INTO nodes (operator_id, node_class, hostname) VALUES ('op-owner-rule', 'C', 'h')`,
		"operator node with a country":               `INSERT INTO nodes (operator_id, node_class, country_code) VALUES ('op-owner-rule', 'C', 'US')`,
		"operator node with a region":                `INSERT INTO nodes (operator_id, node_class, region) VALUES ('op-owner-rule', 'C', 'KY')`,
		"operator node with coordinates":             `INSERT INTO nodes (operator_id, node_class, latitude, longitude) VALUES ('op-owner-rule', 'C', 38.2, -85.7)`,
		"operator node owned by an unknown operator": `INSERT INTO nodes (operator_id, node_class) VALUES ('no-such-operator', 'C')`,
	}
	for name, sql := range refused {
		if _, err := db.Pool.Exec(ctx, sql); err == nil {
			t.Errorf("%s: the database accepted it", name)
		}
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO nodes (operator_id, node_class) VALUES ('op-owner-rule', 'C')`); err != nil {
		t.Errorf("a person-free operator node was refused: %v", err)
	}
	// An operator that still owns nodes cannot be deleted out from under them.
	if _, err := db.Pool.Exec(ctx, `DELETE FROM operators WHERE id = 'op-owner-rule'`); err == nil {
		t.Error("an operator owning nodes was deleted")
	}
}

func TestRegisterOperatorNode_PersonFreeAndKeyed(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	seedOperator(t, db, "op-register")
	nodeID := uuid.NewString()
	pub := newKey(t)

	created, err := store.RegisterOperatorNode(ctx, db, "op-register", nodeID, pub)
	if err != nil || !created {
		t.Fatalf("RegisterOperatorNode = %v, %v; want created", created, err)
	}

	var operatorID string
	var participant, hostname, country, region *string
	var lat, long *float64
	if err := db.Pool.QueryRow(ctx,
		`SELECT operator_id, participant_id::text, hostname, country_code, region, latitude, longitude
		 FROM nodes WHERE id = $1`, nodeID,
	).Scan(&operatorID, &participant, &hostname, &country, &region, &lat, &long); err != nil {
		t.Fatalf("read node: %v", err)
	}
	if operatorID != "op-register" {
		t.Errorf("operator_id = %q", operatorID)
	}
	for name, v := range map[string]*string{"participant_id": participant, "hostname": hostname, "country_code": country, "region": region} {
		if v != nil {
			t.Errorf("%s = %q, want NULL: no person on an operator node", name, *v)
		}
	}
	if lat != nil || long != nil {
		t.Error("coordinates stored on an operator node")
	}
	var stored []byte
	if err := db.Pool.QueryRow(ctx, `SELECT public_key FROM node_protocol_keys WHERE node_id = $1`, nodeID).Scan(&stored); err != nil || !bytes.Equal(stored, pub) {
		t.Fatalf("enrolled key = %x, %v; want the registered key", stored, err)
	}
	owner, err := store.NodeOperator(ctx, db, nodeID)
	if err != nil || owner != "op-register" {
		t.Errorf("NodeOperator = %q, %v", owner, err)
	}
}

func TestRegisterOperatorNode_FirstWriteWins(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	seedOperator(t, db, "op-first")
	seedOperator(t, db, "op-second")
	nodeID := uuid.NewString()
	pub := newKey(t)
	if _, err := store.RegisterOperatorNode(ctx, db, "op-first", nodeID, pub); err != nil {
		t.Fatal(err)
	}

	if created, err := store.RegisterOperatorNode(ctx, db, "op-first", nodeID, pub); err != nil || created {
		t.Errorf("same operator, same key = %v, %v; want idempotent (false, nil)", created, err)
	}
	if _, err := store.RegisterOperatorNode(ctx, db, "op-first", nodeID, newKey(t)); !errors.Is(err, store.ErrNodeKeyConflict) {
		t.Errorf("same node, different key = %v; want ErrNodeKeyConflict", err)
	}
	if _, err := store.RegisterOperatorNode(ctx, db, "op-second", nodeID, pub); !errors.Is(err, store.ErrNodeOwnedElsewhere) {
		t.Errorf("another operator's node = %v; want ErrNodeOwnedElsewhere", err)
	}
	personNode := seedParticipantNode(t, db)
	if _, err := store.RegisterOperatorNode(ctx, db, "op-first", personNode, pub); !errors.Is(err, store.ErrNodeOwnedElsewhere) {
		t.Errorf("a participant's node = %v; want ErrNodeOwnedElsewhere", err)
	}
	// Nothing changed: still op-first's node with the first key, and the
	// participant node has no operator and no key.
	var stored []byte
	if err := db.Pool.QueryRow(ctx, `SELECT public_key FROM node_protocol_keys WHERE node_id = $1`, nodeID).Scan(&stored); err != nil || !bytes.Equal(stored, pub) {
		t.Errorf("key after refused writes = %x, %v", stored, err)
	}
	if owner, _ := store.NodeOperator(ctx, db, personNode); owner != "" {
		t.Errorf("participant node gained operator %q", owner)
	}
	var keys int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM node_protocol_keys WHERE node_id = $1`, personNode).Scan(&keys); err != nil || keys != 0 {
		t.Errorf("participant node gained %d keys (%v)", keys, err)
	}
}

// If the key cannot be stored, no node row is left behind.
func TestRegisterOperatorNode_OneTransaction(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	seedOperator(t, db, "op-atomic")
	if _, err := db.Pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION test_refuse_key() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'test: key insert refused'; END $$ LANGUAGE plpgsql;
		CREATE TRIGGER test_refuse_key BEFORE INSERT ON node_protocol_keys
		FOR EACH ROW EXECUTE FUNCTION test_refuse_key();`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}
	t.Cleanup(func() {
		db.Pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS test_refuse_key ON node_protocol_keys; DROP FUNCTION IF EXISTS test_refuse_key();`) //nolint:errcheck
	})

	nodeID := uuid.NewString()
	if _, err := store.RegisterOperatorNode(ctx, db, "op-atomic", nodeID, newKey(t)); err == nil {
		t.Fatal("registration succeeded although the key insert failed")
	}
	if _, err := store.NodeOperator(ctx, db, nodeID); !errors.Is(err, store.ErrNodeNotFound) {
		t.Errorf("node row left behind after the key failed: %v", err)
	}
}

// An operator-owned node never reaches the payout releaser: SoHoLINK pays no
// prosumer (decision 0002); EligiblePayouts joins through participants.
func TestEligiblePayouts_SkipsOperatorNodes(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	seedOperator(t, db, "op-nopay")
	nodeID := uuid.NewString()
	if _, err := store.RegisterOperatorNode(ctx, db, "op-nopay", nodeID, newKey(t)); err != nil {
		t.Fatal(err)
	}
	var consumerID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO participants (email, display_name) VALUES ('nopay-consumer@test.com', 'C') RETURNING id`,
	).Scan(&consumerID); err != nil {
		t.Fatal(err)
	}
	completedAt := time.Now().Add(-25 * time.Hour)
	var jobID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO jobs (participant_id, node_id, workload_type, status, amount_cents, cpu_cores, ram_mb, started_at, completed_at)
		 VALUES ($1, $2, 'app_hosting', 'completed', 5000, 2, 4096, $3, $4) RETURNING id`,
		consumerID, nodeID, completedAt.Add(-time.Hour), completedAt,
	).Scan(&jobID); err != nil {
		t.Fatalf("seed job: %v", err)
	}
	if _, err := db.Pool.Exec(ctx,
		`INSERT INTO job_metering (job_id, cpu_core_hours, ram_gb_hours, storage_gb_months,
		  consumer_paid_cents, contributor_earned_cents, platform_fee_cents)
		 VALUES ($1, 1.0, 4.0, 0.0, 5000, 3250, 1750)`, jobID,
	); err != nil {
		t.Fatalf("seed metering: %v", err)
	}
	t.Cleanup(func() { db.Pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, jobID) }) //nolint:errcheck

	candidates, err := store.EligiblePayouts(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range candidates {
		if c.JobID == jobID {
			t.Fatal("a job on an operator-owned node is eligible for a payout")
		}
	}
}
