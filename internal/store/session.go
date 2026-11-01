package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Session stages. A session is only fully authenticated in StageFull.
const (
	// StagePassword means the password was accepted but a second factor is
	// still pending.
	StagePassword = "password"
	// StageEnroll means the admin authenticated but has no registered second
	// factor, so they must enroll one before the session becomes StageFull.
	StageEnroll = "enroll"
	// StageFull is a fully authenticated session.
	StageFull = "full"
)

type Session struct {
	ID         string
	AdminID    string
	Stage      string
	CSRFToken  string
	CreatedAt  int64
	ExpiresAt  int64
	LastSeenAt int64
	UserAgent  string
	IP         string

	TOTPPendingSecret    string
	TOTPPendingCreatedAt int64

	WebAuthnRegisterSession string
	WebAuthnLoginSession    string
	PasskeyPendingName      string
}

func (s *Store) CreateSession(ctx context.Context, session Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, admin_id, stage, csrf_token, created_at, expires_at, last_seen_at, user_agent, ip)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.AdminID, session.Stage, session.CSRFToken,
		session.CreatedAt, session.ExpiresAt, session.LastSeenAt, session.UserAgent, session.IP,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	return nil
}

func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	var (
		session    Session
		pending    sql.NullString
		pendingAt  sql.NullInt64
		register   sql.NullString
		login      sql.NullString
		pendingKey sql.NullString
	)

	err := s.db.QueryRowContext(ctx,
		`SELECT id, admin_id, stage, csrf_token, created_at, expires_at, last_seen_at, user_agent, ip,
		        totp_pending_secret, totp_pending_created_at, webauthn_register_session, webauthn_login_session, passkey_pending_name
		 FROM sessions WHERE id = ?`, id,
	).Scan(
		&session.ID, &session.AdminID, &session.Stage, &session.CSRFToken,
		&session.CreatedAt, &session.ExpiresAt, &session.LastSeenAt, &session.UserAgent, &session.IP,
		&pending, &pendingAt, &register, &login, &pendingKey,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	session.TOTPPendingSecret = pending.String
	session.TOTPPendingCreatedAt = pendingAt.Int64
	session.WebAuthnRegisterSession = register.String
	session.WebAuthnLoginSession = login.String
	session.PasskeyPendingName = pendingKey.String

	return &session, nil
}

func (s *Store) TouchSession(ctx context.Context, id string, lastSeenAt int64) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ? WHERE id = ?`, lastSeenAt, id)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}

	return expectAffected(result, "session")
}

// PromoteSession moves a session to a new stage and rotates its CSRF token.
func (s *Store) PromoteSession(ctx context.Context, id, stage, csrfToken string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET stage = ?, csrf_token = ? WHERE id = ?`, stage, csrfToken, id)
	if err != nil {
		return fmt.Errorf("promote session: %w", err)
	}

	return expectAffected(result, "session")
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	return nil
}

// DeleteOtherSessions removes every session for an admin except keepID.
func (s *Store) DeleteOtherSessions(ctx context.Context, adminID, keepID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE admin_id = ? AND id <> ?`, adminID, keepID)
	if err != nil {
		return fmt.Errorf("delete other sessions: %w", err)
	}

	return nil
}

// DeleteStaleSessions removes sessions that are past their absolute expiry or
// have been idle for longer than idleMillis.
func (s *Store) DeleteStaleSessions(ctx context.Context, now, idleMillis int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= ? OR last_seen_at <= ?`, now, now-idleMillis)
	if err != nil {
		return fmt.Errorf("delete stale sessions: %w", err)
	}

	return nil
}

func (s *Store) SetTOTPPending(ctx context.Context, id, secret string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET totp_pending_secret = ?, totp_pending_created_at = ? WHERE id = ?`, secret, Now(), id)
	if err != nil {
		return fmt.Errorf("set TOTP pending secret: %w", err)
	}

	return expectAffected(result, "session")
}

func (s *Store) ClearTOTPPending(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET totp_pending_secret = NULL, totp_pending_created_at = NULL WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("clear TOTP pending secret: %w", err)
	}

	return expectAffected(result, "session")
}

func (s *Store) SetWebAuthnRegisterSession(ctx context.Context, id, data string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET webauthn_register_session = ? WHERE id = ?`, data, id)
	if err != nil {
		return fmt.Errorf("set webauthn registration session: %w", err)
	}

	return expectAffected(result, "session")
}

func (s *Store) ClearWebAuthnRegisterSession(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET webauthn_register_session = NULL, passkey_pending_name = NULL WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("clear webauthn registration session: %w", err)
	}

	return expectAffected(result, "session")
}

func (s *Store) SetWebAuthnLoginSession(ctx context.Context, id, data string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET webauthn_login_session = ? WHERE id = ?`, data, id)
	if err != nil {
		return fmt.Errorf("set webauthn login session: %w", err)
	}

	return expectAffected(result, "session")
}

func (s *Store) ClearWebAuthnLoginSession(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET webauthn_login_session = NULL WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("clear webauthn login session: %w", err)
	}

	return expectAffected(result, "session")
}

func (s *Store) SetPasskeyPendingName(ctx context.Context, id, name string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET passkey_pending_name = ? WHERE id = ?`, name, id)
	if err != nil {
		return fmt.Errorf("set pending passkey name: %w", err)
	}

	return expectAffected(result, "session")
}
