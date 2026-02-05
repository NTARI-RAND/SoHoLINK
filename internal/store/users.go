package store

import (
	"context"
	"database/sql"
	"fmt"
)

// AddUser creates a new user in the database.
func (s *Store) AddUser(ctx context.Context, username, did string, publicKey []byte, role string) error {
	if role == "" {
		role = "basic"
	}

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO users (username, did, public_key, role) VALUES (?, ?, ?, ?)",
		username, did, publicKey, role)
	if err != nil {
		return fmt.Errorf("failed to add user: %w", err)
	}
	return nil
}

// GetUserByUsername retrieves a user by their username.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		"SELECT id, username, did, public_key, role, created_at, revoked_at FROM users WHERE username = ?",
		username).Scan(&u.ID, &u.Username, &u.DID, &u.PublicKey, &u.Role, &u.CreatedAt, &u.RevokedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user by username: %w", err)
	}
	return &u, nil
}

// GetUserByDID retrieves a user by their DID.
func (s *Store) GetUserByDID(ctx context.Context, did string) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		"SELECT id, username, did, public_key, role, created_at, revoked_at FROM users WHERE did = ?",
		did).Scan(&u.ID, &u.Username, &u.DID, &u.PublicKey, &u.Role, &u.CreatedAt, &u.RevokedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user by DID: %w", err)
	}
	return &u, nil
}

// ListUsers returns all users in the database.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, username, did, public_key, role, created_at, revoked_at FROM users ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.DID, &u.PublicKey, &u.Role, &u.CreatedAt, &u.RevokedAt); err != nil {
			return nil, fmt.Errorf("failed to scan user row: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// RevokeUser marks a user as revoked by updating their revoked_at field
// and inserting a revocation record.
func (s *Store) RevokeUser(ctx context.Context, username, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get user DID
	var did string
	err = tx.QueryRowContext(ctx,
		"SELECT did FROM users WHERE username = ?", username).Scan(&did)
	if err == sql.ErrNoRows {
		return fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		return fmt.Errorf("failed to find user: %w", err)
	}

	// Update user record
	_, err = tx.ExecContext(ctx,
		"UPDATE users SET revoked_at = CURRENT_TIMESTAMP WHERE username = ?", username)
	if err != nil {
		return fmt.Errorf("failed to update user revocation: %w", err)
	}

	// Insert revocation record
	_, err = tx.ExecContext(ctx,
		"INSERT INTO revocations (did, reason) VALUES (?, ?)", did, reason)
	if err != nil {
		return fmt.Errorf("failed to insert revocation record: %w", err)
	}

	return tx.Commit()
}

// UserCount returns the total number of users.
func (s *Store) UserCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

// ActiveUserCount returns the number of non-revoked users.
func (s *Store) ActiveUserCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM users WHERE revoked_at IS NULL").Scan(&count)
	return count, err
}
