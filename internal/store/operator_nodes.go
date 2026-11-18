package store

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrNodeOwnedElsewhere is returned by RegisterOperatorNode when the node
	// ID already belongs to another operator or to a participant.
	ErrNodeOwnedElsewhere = errors.New("store: node is owned by someone else")
	// ErrNodeKeyConflict is returned by RegisterOperatorNode when the node is
	// already enrolled with a different public key. Rotation is an operator
	// action with sign-off, never an overwrite (the /nodes/pubkey rule).
	ErrNodeKeyConflict = errors.New("store: node already enrolled with a different key")
)

// RegisterOperatorNode records a node owned by operatorID, with no person
// attached, and enrolls its ed25519 protocol key — both in one transaction, so
// a node never exists without its key. The node starts as class C and offline;
// its first signed capability listing sets the class.
//
// Idempotent for the same operator and key (created=false, err=nil). A node ID
// owned by another operator or by a participant returns ErrNodeOwnedElsewhere;
// the same node with a different key returns ErrNodeKeyConflict. Neither
// changes anything.
func RegisterOperatorNode(ctx context.Context, db *DB, operatorID, nodeID string, pub ed25519.PublicKey) (created bool, err error) {
	if len(pub) != ed25519.PublicKeySize {
		return false, fmt.Errorf("register operator node %s: public key must be %d bytes", nodeID, ed25519.PublicKeySize)
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("register operator node %s: begin: %w", nodeID, err)
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck // no-op after Commit

	tag, err := tx.Exec(ctx,
		`INSERT INTO nodes (id, operator_id, node_class, status)
		 VALUES ($1, $2, 'C'::node_class, 'offline'::node_status)
		 ON CONFLICT (id) DO NOTHING`,
		nodeID, operatorID,
	)
	if err != nil {
		return false, fmt.Errorf("register operator node %s: insert node: %w", nodeID, err)
	}
	created = tag.RowsAffected() == 1
	if !created {
		var owner string
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(operator_id, '') FROM nodes WHERE id = $1`, nodeID,
		).Scan(&owner); err != nil {
			return false, fmt.Errorf("register operator node %s: read owner: %w", nodeID, err)
		}
		if owner != operatorID {
			return false, ErrNodeOwnedElsewhere
		}
	}

	tag, err = tx.Exec(ctx,
		`INSERT INTO node_protocol_keys (node_id, public_key, algo)
		 VALUES ($1, $2, 'ed25519')
		 ON CONFLICT (node_id) DO NOTHING`,
		nodeID, []byte(pub),
	)
	if err != nil {
		return false, fmt.Errorf("register operator node %s: enroll key: %w", nodeID, err)
	}
	if tag.RowsAffected() == 0 {
		var existing []byte
		if err := tx.QueryRow(ctx,
			`SELECT public_key FROM node_protocol_keys WHERE node_id = $1`, nodeID,
		).Scan(&existing); err != nil {
			return false, fmt.Errorf("register operator node %s: read key: %w", nodeID, err)
		}
		if !bytes.Equal(existing, pub) {
			return false, ErrNodeKeyConflict
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("register operator node %s: commit: %w", nodeID, err)
	}
	return created, nil
}

// NodeOperator returns the operator that owns nodeID, or "" for a
// participant-owned node. ErrNodeNotFound if the node does not exist.
func NodeOperator(ctx context.Context, db *DB, nodeID string) (string, error) {
	var owner string
	err := db.Pool.QueryRow(ctx,
		`SELECT COALESCE(operator_id, '') FROM nodes WHERE id = $1`, nodeID,
	).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("node operator %s: %w", nodeID, ErrNodeNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("node operator %s: %w", nodeID, err)
	}
	return owner, nil
}
