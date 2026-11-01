package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type TOTPCredential struct {
	AdminID    string
	Secret     string
	CreatedAt  int64
	LastUsedAt *int64
}

func (s *Store) UpsertTOTP(ctx context.Context, adminID, secret string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO totp_credentials (admin_id, secret, created_at) VALUES (?, ?, ?)
		 ON CONFLICT (admin_id) DO UPDATE SET secret = excluded.secret, created_at = excluded.created_at, last_used_at = NULL`,
		adminID, secret, Now(),
	)
	if err != nil {
		return fmt.Errorf("upsert TOTP credential: %w", err)
	}

	return nil
}

func (s *Store) GetTOTP(ctx context.Context, adminID string) (*TOTPCredential, error) {
	var (
		credential TOTPCredential
		lastUsed   sql.NullInt64
	)

	err := s.db.QueryRowContext(ctx,
		`SELECT admin_id, secret, created_at, last_used_at FROM totp_credentials WHERE admin_id = ?`, adminID,
	).Scan(&credential.AdminID, &credential.Secret, &credential.CreatedAt, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get TOTP credential: %w", err)
	}

	if lastUsed.Valid {
		credential.LastUsedAt = &lastUsed.Int64
	}

	return &credential, nil
}

func (s *Store) TouchTOTP(ctx context.Context, adminID string, at int64) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE totp_credentials SET last_used_at = ? WHERE admin_id = ?`, at, adminID); err != nil {
		return fmt.Errorf("touch TOTP credential: %w", err)
	}

	return nil
}

func (s *Store) DeleteTOTP(ctx context.Context, adminID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM totp_credentials WHERE admin_id = ?`, adminID)
	if err != nil {
		return fmt.Errorf("delete TOTP credential: %w", err)
	}

	return expectAffected(result, "TOTP credential")
}

type WebAuthnCredential struct {
	// ID is the base64url encoded credential ID.
	ID         string
	AdminID    string
	Name       string
	Data       string
	CreatedAt  int64
	LastUsedAt *int64
}

func (s *Store) CreateWebAuthnCredential(ctx context.Context, credential WebAuthnCredential) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webauthn_credentials (id, admin_id, name, data, created_at) VALUES (?, ?, ?, ?, ?)`,
		credential.ID, credential.AdminID, credential.Name, credential.Data, orNow(credential.CreatedAt),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("passkey: %w", ErrConflict)
		}

		return fmt.Errorf("create passkey: %w", err)
	}

	return nil
}

func (s *Store) ListWebAuthnCredentials(ctx context.Context, adminID string) ([]WebAuthnCredential, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, admin_id, name, data, created_at, last_used_at FROM webauthn_credentials WHERE admin_id = ? ORDER BY created_at ASC`,
		adminID)
	if err != nil {
		return nil, fmt.Errorf("list passkeys: %w", err)
	}
	defer rows.Close()

	credentials := make([]WebAuthnCredential, 0, 4)
	for rows.Next() {
		var (
			credential WebAuthnCredential
			lastUsed   sql.NullInt64
		)
		if err := rows.Scan(&credential.ID, &credential.AdminID, &credential.Name, &credential.Data, &credential.CreatedAt, &lastUsed); err != nil {
			return nil, fmt.Errorf("scan passkey: %w", err)
		}
		if lastUsed.Valid {
			credential.LastUsedAt = &lastUsed.Int64
		}
		credentials = append(credentials, credential)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate passkeys: %w", err)
	}

	return credentials, nil
}

func (s *Store) GetWebAuthnCredential(ctx context.Context, id string) (*WebAuthnCredential, error) {
	var (
		credential WebAuthnCredential
		lastUsed   sql.NullInt64
	)

	err := s.db.QueryRowContext(ctx,
		`SELECT id, admin_id, name, data, created_at, last_used_at FROM webauthn_credentials WHERE id = ?`, id,
	).Scan(&credential.ID, &credential.AdminID, &credential.Name, &credential.Data, &credential.CreatedAt, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get passkey: %w", err)
	}

	if lastUsed.Valid {
		credential.LastUsedAt = &lastUsed.Int64
	}

	return &credential, nil
}

func (s *Store) UpdateWebAuthnCredential(ctx context.Context, id, data string, lastUsedAt int64) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE webauthn_credentials SET data = ?, last_used_at = ? WHERE id = ?`, data, lastUsedAt, id)
	if err != nil {
		return fmt.Errorf("update passkey: %w", err)
	}

	return expectAffected(result, "passkey")
}

func (s *Store) DeleteWebAuthnCredential(ctx context.Context, adminID, id string) error {
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM webauthn_credentials WHERE id = ? AND admin_id = ?`, id, adminID)
	if err != nil {
		return fmt.Errorf("delete passkey: %w", err)
	}

	return expectAffected(result, "passkey")
}

// orNow returns t when it is set, otherwise the current time.
func orNow(t int64) int64 {
	if t != 0 {
		return t
	}

	return Now()
}
