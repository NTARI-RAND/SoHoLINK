package store

import (
	"context"
	"time"
)

// UpsertFCMToken stores or updates the FCM device token for a node.
// Called when a mobile client POSTs to /api/v1/nodes/mobile/fcm-token.
func (s *Store) UpsertFCMToken(ctx context.Context, nodeDID, fcmToken string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO device_fcm_tokens (node_did, fcm_token, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(node_did) DO UPDATE SET
			fcm_token  = excluded.fcm_token,
			updated_at = excluded.updated_at`,
		nodeDID, fcmToken, time.Now().UTC(),
	)
	return err
}

// GetFCMToken returns the FCM token for a node, or ("", nil) if not found.
func (s *Store) GetFCMToken(ctx context.Context, nodeDID string) (string, error) {
	var token string
	err := s.db.QueryRowContext(ctx,
		"SELECT fcm_token FROM device_fcm_tokens WHERE node_did = ?", nodeDID,
	).Scan(&token)
	if err != nil {
		// sql.ErrNoRows is not an error — just no token registered.
		return "", nil
	}
	return token, nil
}

// DeleteFCMToken removes a node's FCM token (e.g. after UNREGISTERED error).
func (s *Store) DeleteFCMToken(ctx context.Context, nodeDID string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM device_fcm_tokens WHERE node_did = ?", nodeDID)
	return err
}
