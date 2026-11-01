package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Audit actions recorded by the service.
const (
	ActionLoginSucceeded      = "auth.login.succeeded"
	ActionLoginFailed         = "auth.login.failed"
	ActionLogout              = "auth.logout"
	ActionPasswordChanged     = "auth.password.changed"
	ActionTOTPEnrolled        = "auth.totp.enrolled"
	ActionTOTPRemoved         = "auth.totp.removed"
	ActionPasskeyEnrolled     = "auth.passkey.enrolled"
	ActionPasskeyRemoved      = "auth.passkey.removed"
	ActionInviteCreated       = "admin.invite.created"
	ActionInviteRevoked       = "admin.invite.revoked"
	ActionInviteAccepted      = "admin.invite.accepted"
	ActionAdminRemoved        = "admin.removed"
	ActionSubscriptionAdded   = "subscription.added"
	ActionSubscriptionRemoved = "subscription.removed"
	ActionDeliveryFailed      = "notification.delivery.failed"
)

type AuditEntry struct {
	ID        int64
	At        int64
	Actor     string
	Action    string
	Target    string
	Details   string
	IP        string
	UserAgent string
}

func (s *Store) InsertAudit(ctx context.Context, entry AuditEntry) error {
	if entry.At == 0 {
		entry.At = Now()
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (at, actor, action, target, details, ip, user_agent) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.At, entry.Actor, entry.Action, nullIfEmpty(entry.Target), nullIfEmpty(entry.Details),
		nullIfEmpty(entry.IP), nullIfEmpty(entry.UserAgent),
	)
	if err != nil {
		return fmt.Errorf("insert audit entry: %w", err)
	}

	return nil
}

func (s *Store) ListAudit(ctx context.Context, limit, offset int) ([]AuditEntry, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM audit_log`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit entries: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, at, actor, action, target, details, ip, user_agent FROM audit_log ORDER BY at DESC, id DESC LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit entries: %w", err)
	}
	defer rows.Close()

	entries := make([]AuditEntry, 0, limit)
	for rows.Next() {
		var (
			entry     AuditEntry
			target    sql.NullString
			details   sql.NullString
			ip        sql.NullString
			userAgent sql.NullString
		)
		if err := rows.Scan(&entry.ID, &entry.At, &entry.Actor, &entry.Action, &target, &details, &ip, &userAgent); err != nil {
			return nil, 0, fmt.Errorf("scan audit entry: %w", err)
		}
		entry.Target = target.String
		entry.Details = details.String
		entry.IP = ip.String
		entry.UserAgent = userAgent.String
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate audit entries: %w", err)
	}

	return entries, total, nil
}
