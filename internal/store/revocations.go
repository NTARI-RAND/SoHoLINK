package store

import (
	"context"
	"fmt"
	"time"
)

// IsRevoked checks if a user DID is in the revocation list.
func (s *Store) IsRevoked(ctx context.Context, did string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM revocations WHERE did = ?", did).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check revocation: %w", err)
	}
	return count > 0, nil
}

// CheckNonce returns true if the nonce has already been seen (replay attempt).
func (s *Store) CheckNonce(ctx context.Context, nonce string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM nonce_cache WHERE nonce = ?", nonce).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check nonce: %w", err)
	}
	return count > 0, nil
}

// RecordNonce records a nonce as seen to prevent replay attacks.
func (s *Store) RecordNonce(ctx context.Context, nonce string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO nonce_cache (nonce) VALUES (?)", nonce)
	if err != nil {
		return fmt.Errorf("failed to record nonce: %w", err)
	}
	return nil
}

// PruneExpiredNonces removes nonces older than maxAge.
func (s *Store) PruneExpiredNonces(ctx context.Context, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge).UTC().Format("2006-01-02 15:04:05")

	result, err := s.db.ExecContext(ctx,
		"DELETE FROM nonce_cache WHERE seen_at < ?", cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to prune nonces: %w", err)
	}

	return result.RowsAffected()
}

// RevocationCount returns the total number of revocations.
func (s *Store) RevocationCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM revocations").Scan(&count)
	return count, err
}
