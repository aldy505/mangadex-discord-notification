package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const (
	KindMangadex = "mangadex"
	KindRSS      = "rss"
)

type Subscription struct {
	ID              string
	Kind            string
	Label           string
	Source          string
	Webhook         string
	Languages       string
	IntervalSeconds int64
	Enabled         bool
	Watermark       *int64
	LastCheckedAt   *int64
	LastError       string
	CreatedBy       string
	CreatedAt       int64
}

// LanguageList splits the stored comma separated language list.
func (s Subscription) LanguageList() []string {
	if strings.TrimSpace(s.Languages) == "" {
		return []string{}
	}

	parts := strings.Split(s.Languages, ",")
	languages := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			languages = append(languages, part)
		}
	}

	return languages
}

// SetLanguages stores a normalized comma separated language list.
func (s *Subscription) SetLanguages(languages []string) {
	cleaned := make([]string, 0, len(languages))
	seen := make(map[string]struct{}, len(languages))
	for _, language := range languages {
		language = strings.ToLower(strings.TrimSpace(language))
		if language == "" {
			continue
		}
		if _, ok := seen[language]; ok {
			continue
		}
		seen[language] = struct{}{}
		cleaned = append(cleaned, language)
	}
	s.Languages = strings.Join(cleaned, ",")
}

func (s *Store) CreateSubscription(ctx context.Context, subscription *Subscription) error {
	if subscription.CreatedAt == 0 {
		subscription.CreatedAt = Now()
	}

	var createdBy any
	if subscription.CreatedBy != "" {
		createdBy = subscription.CreatedBy
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO subscriptions (id, kind, label, source, webhook, languages, interval_seconds, enabled, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		subscription.ID, subscription.Kind, subscription.Label, subscription.Source, subscription.Webhook,
		subscription.Languages, subscription.IntervalSeconds, boolToInt(subscription.Enabled), createdBy, subscription.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("subscription for %s: %w", subscription.Source, ErrConflict)
		}

		return fmt.Errorf("create subscription: %w", err)
	}

	return nil
}

func (s *Store) GetSubscription(ctx context.Context, id string) (*Subscription, error) {
	return scanSubscription(s.db.QueryRowContext(ctx,
		`SELECT id, kind, label, source, webhook, languages, interval_seconds, enabled, watermark,
		        last_checked_at, last_error, created_by, created_at
		 FROM subscriptions WHERE id = ?`, id))
}

func (s *Store) ListSubscriptions(ctx context.Context) ([]Subscription, error) {
	return s.querySubscriptions(ctx,
		`SELECT id, kind, label, source, webhook, languages, interval_seconds, enabled, watermark,
		        last_checked_at, last_error, created_by, created_at
		 FROM subscriptions ORDER BY created_at DESC`)
}

func (s *Store) ListEnabledSubscriptions(ctx context.Context) ([]Subscription, error) {
	return s.querySubscriptions(ctx,
		`SELECT id, kind, label, source, webhook, languages, interval_seconds, enabled, watermark,
		        last_checked_at, last_error, created_by, created_at
		 FROM subscriptions WHERE enabled = 1 ORDER BY created_at ASC`)
}

func (s *Store) querySubscriptions(ctx context.Context, query string) ([]Subscription, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	defer rows.Close()

	subscriptions := make([]Subscription, 0, 16)
	for rows.Next() {
		subscription, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, *subscription)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriptions: %w", err)
	}

	return subscriptions, nil
}

func scanSubscription(row rowScanner) (*Subscription, error) {
	var (
		subscription  Subscription
		enabled       int
		watermark     sql.NullInt64
		lastCheckedAt sql.NullInt64
		lastError     sql.NullString
		createdBy     sql.NullString
	)

	err := row.Scan(
		&subscription.ID, &subscription.Kind, &subscription.Label, &subscription.Source, &subscription.Webhook,
		&subscription.Languages, &subscription.IntervalSeconds, &enabled, &watermark, &lastCheckedAt, &lastError,
		&createdBy, &subscription.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan subscription: %w", err)
	}

	subscription.Enabled = enabled == 1
	subscription.LastError = lastError.String
	subscription.CreatedBy = createdBy.String
	if watermark.Valid {
		subscription.Watermark = &watermark.Int64
	}
	if lastCheckedAt.Valid {
		subscription.LastCheckedAt = &lastCheckedAt.Int64
	}

	return &subscription, nil
}

func (s *Store) DeleteSubscription(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM subscriptions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}

	return expectAffected(result, "subscription")
}

// UpdateSubscriptionPoll records the result of one polling round.
func (s *Store) UpdateSubscriptionPoll(ctx context.Context, id string, watermark *int64, checkedAt int64, lastError string) error {
	query := `UPDATE subscriptions SET last_checked_at = ?, last_error = ? WHERE id = ?`
	args := []any{checkedAt, nullIfEmpty(lastError), id}

	if watermark != nil {
		query = `UPDATE subscriptions SET last_checked_at = ?, last_error = ?, watermark = ? WHERE id = ?`
		args = []any{checkedAt, nullIfEmpty(lastError), *watermark, id}
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update subscription poll state: %w", err)
	}

	return expectAffected(result, "subscription")
}

func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}

	return value
}
