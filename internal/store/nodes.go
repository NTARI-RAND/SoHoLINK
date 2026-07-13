package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrNodeNotFound is returned by UpdateNodeCapabilities when the node id has
// no row. The protocol adapter maps this to "unknown node": SubmitListing
// updates capabilities on an EXISTING node only — bespoke /nodes/claim
// remains the onboarding path transitionally.
var ErrNodeNotFound = errors.New("store: node not found")

// NodePlacementRow carries the identity/geo columns the in-memory registry
// needs when refreshing a node entry from a capability listing.
type NodePlacementRow struct {
	ParticipantID string
	CountryCode   string
	Region        string
}

// UpdateNodeCapabilities updates a node's certified class and merges the
// listed capacity into its hardware_profile JSONB. The merge (||) touches
// only cpu_cores / ram_mb / storage_gb — gpu_present and bandwidth_mbps are
// unknown to a protocol CapabilityListing and are preserved as-is.
func UpdateNodeCapabilities(ctx context.Context, db *DB, nodeID, nodeClass string, cpuCores, ramMB, storageGB int) (NodePlacementRow, error) {
	var row NodePlacementRow
	err := db.Pool.QueryRow(ctx,
		`UPDATE nodes
		 SET node_class       = $2::node_class,
		     hardware_profile = hardware_profile || jsonb_build_object(
		         'cpu_cores', $3::int, 'ram_mb', $4::int, 'storage_gb', $5::int),
		     updated_at       = NOW()
		 WHERE id = $1
		 RETURNING participant_id::text, country_code, COALESCE(region, '')`,
		nodeID, nodeClass, cpuCores, ramMB, storageGB,
	).Scan(&row.ParticipantID, &row.CountryCode, &row.Region)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return NodePlacementRow{}, fmt.Errorf("update node capabilities %s: %w", nodeID, ErrNodeNotFound)
		}
		return NodePlacementRow{}, fmt.Errorf("update node capabilities %s: %w", nodeID, err)
	}
	return row, nil
}
