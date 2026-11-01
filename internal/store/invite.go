package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type Invite struct {
	ID        string
	TokenHash string
	Email     string
	// CreatedBy is empty for invites created from the CLI.
	CreatedBy string
	CreatedAt int64
	ExpiresAt int64
	UsedAt    *int64
	RevokedAt *int64
}

// State summarizes an invite for display.
func (i Invite) State(now int64) string {
	switch {
	case i.UsedAt != nil:
		return InviteStateUsed
	case i.RevokedAt != nil:
		return InviteStateRevoked
	case now >= i.ExpiresAt:
		return InviteStateExpired
	default:
		return InviteStatePending
	}
}

const (
	InviteStatePending = "pending"
	InviteStateUsed    = "used"
	InviteStateExpired = "expired"
	InviteStateRevoked = "revoked"
)

func (s *Store) CreateInvite(ctx context.Context, invite Invite) error {
	var createdBy any
	if invite.CreatedBy != "" {
		createdBy = invite.CreatedBy
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO invites (id, token_hash, email, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		invite.ID, invite.TokenHash, invite.Email, createdBy, invite.CreatedAt, invite.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create invite: %w", err)
	}

	return nil
}

func (s *Store) GetInviteByTokenHash(ctx context.Context, tokenHash string) (*Invite, error) {
	return s.scanInvite(s.db.QueryRowContext(ctx,
		`SELECT id, token_hash, email, created_by, created_at, expires_at, used_at, revoked_at
		 FROM invites WHERE token_hash = ?`, tokenHash))
}

func (s *Store) GetInviteByID(ctx context.Context, id string) (*Invite, error) {
	return s.scanInvite(s.db.QueryRowContext(ctx,
		`SELECT id, token_hash, email, created_by, created_at, expires_at, used_at, revoked_at
		 FROM invites WHERE id = ?`, id))
}

func (s *Store) scanInvite(row rowScanner) (*Invite, error) {
	var (
		invite    Invite
		createdBy sql.NullString
		usedAt    sql.NullInt64
		revokedAt sql.NullInt64
	)

	err := row.Scan(&invite.ID, &invite.TokenHash, &invite.Email, &createdBy, &invite.CreatedAt, &invite.ExpiresAt, &usedAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan invite: %w", err)
	}

	invite.CreatedBy = createdBy.String
	if usedAt.Valid {
		invite.UsedAt = &usedAt.Int64
	}
	if revokedAt.Valid {
		invite.RevokedAt = &revokedAt.Int64
	}

	return &invite, nil
}

// ConsumeInvite marks a pending invite as used, but only if it is still
// pending. It reports false when another request already consumed, revoked, or
// expired it.
func (s *Store) ConsumeInvite(ctx context.Context, id string, now int64) (bool, error) {
	result, err := s.db.ExecContext(ctx,
		`UPDATE invites SET used_at = ? WHERE id = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?`,
		now, id, now,
	)
	if err != nil {
		return false, fmt.Errorf("consume invite: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}

	return affected > 0, nil
}

func (s *Store) RevokeInvite(ctx context.Context, id string, now int64) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE invites SET revoked_at = ? WHERE id = ? AND used_at IS NULL AND revoked_at IS NULL`, now, id)
	if err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}

	return expectAffected(result, "invite")
}

func (s *Store) ListInvites(ctx context.Context) ([]Invite, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, token_hash, email, created_by, created_at, expires_at, used_at, revoked_at
		 FROM invites ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer rows.Close()

	invites := make([]Invite, 0, 8)
	for rows.Next() {
		var (
			invite    Invite
			createdBy sql.NullString
			usedAt    sql.NullInt64
			revokedAt sql.NullInt64
		)
		if err := rows.Scan(&invite.ID, &invite.TokenHash, &invite.Email, &createdBy, &invite.CreatedAt, &invite.ExpiresAt, &usedAt, &revokedAt); err != nil {
			return nil, fmt.Errorf("scan invite: %w", err)
		}
		invite.CreatedBy = createdBy.String
		if usedAt.Valid {
			invite.UsedAt = &usedAt.Int64
		}
		if revokedAt.Valid {
			invite.RevokedAt = &revokedAt.Int64
		}
		invites = append(invites, invite)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate invites: %w", err)
	}

	return invites, nil
}
