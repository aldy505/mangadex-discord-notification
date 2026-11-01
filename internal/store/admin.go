package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type Admin struct {
	ID           string
	Email        string
	PasswordHash string
	CreatedAt    int64
	UpdatedAt    int64
}

func (s *Store) CreateAdmin(ctx context.Context, id, email, passwordHash string) (*Admin, error) {
	now := Now()

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO admins (id, email, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		id, email, passwordHash, now, now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("admin %s: %w", email, ErrConflict)
		}

		return nil, fmt.Errorf("create admin: %w", err)
	}

	return &Admin{ID: id, Email: email, PasswordHash: passwordHash, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) GetAdminByEmail(ctx context.Context, email string) (*Admin, error) {
	return s.scanAdmin(s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, created_at, updated_at FROM admins WHERE email = ?`, email))
}

func (s *Store) GetAdminByID(ctx context.Context, id string) (*Admin, error) {
	return s.scanAdmin(s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, created_at, updated_at FROM admins WHERE id = ?`, id))
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanAdmin(row rowScanner) (*Admin, error) {
	admin := &Admin{}
	err := row.Scan(&admin.ID, &admin.Email, &admin.PasswordHash, &admin.CreatedAt, &admin.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan admin: %w", err)
	}

	return admin, nil
}

func (s *Store) UpdateAdminPassword(ctx context.Context, id, passwordHash string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE admins SET password_hash = ?, updated_at = ? WHERE id = ?`, passwordHash, Now(), id)
	if err != nil {
		return fmt.Errorf("update admin password: %w", err)
	}

	return expectAffected(result, "admin")
}

func (s *Store) DeleteAdmin(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM admins WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete admin: %w", err)
	}

	return expectAffected(result, "admin")
}

func (s *Store) ListAdmins(ctx context.Context) ([]Admin, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, email, password_hash, created_at, updated_at FROM admins ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list admins: %w", err)
	}
	defer rows.Close()

	admins := make([]Admin, 0, 8)
	for rows.Next() {
		admin := Admin{}
		if err := rows.Scan(&admin.ID, &admin.Email, &admin.PasswordHash, &admin.CreatedAt, &admin.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan admin: %w", err)
		}
		admins = append(admins, admin)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admins: %w", err)
	}

	return admins, nil
}

func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM admins`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}

	return count, nil
}

// AdminFactors reports which second factors an admin has registered.
func (s *Store) AdminFactors(ctx context.Context, adminID string) (hasTOTP bool, passkeys int, err error) {
	var totpCount int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM totp_credentials WHERE admin_id = ?`, adminID).Scan(&totpCount); err != nil {
		return false, 0, fmt.Errorf("count TOTP credentials: %w", err)
	}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM webauthn_credentials WHERE admin_id = ?`, adminID).Scan(&passkeys); err != nil {
		return false, 0, fmt.Errorf("count passkeys: %w", err)
	}

	return totpCount > 0, passkeys, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}

func expectAffected(result sql.Result, entity string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}

	return nil
}
