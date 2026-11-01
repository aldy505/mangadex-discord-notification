package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()

	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})

	return st
}

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	first, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer second.Close()

	if _, err := second.CountAdmins(context.Background()); err != nil {
		t.Fatalf("CountAdmins after reopen: %v", err)
	}
}

func TestAdminLifecycle(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)

	admin, err := st.CreateAdmin(ctx, "id-1", "admin@example.com", "hash")
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	if admin.CreatedAt == 0 {
		t.Error("expected a creation timestamp")
	}

	if _, err := st.CreateAdmin(ctx, "id-2", "admin@example.com", "hash"); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for a duplicate email, got %v", err)
	}

	found, err := st.GetAdminByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("GetAdminByEmail: %v", err)
	}
	if found.ID != "id-1" {
		t.Errorf("id = %q", found.ID)
	}

	if err := st.UpdateAdminPassword(ctx, "id-1", "new-hash"); err != nil {
		t.Fatalf("UpdateAdminPassword: %v", err)
	}
	found, err = st.GetAdminByID(ctx, "id-1")
	if err != nil {
		t.Fatalf("GetAdminByID: %v", err)
	}
	if found.PasswordHash != "new-hash" {
		t.Errorf("password hash = %q", found.PasswordHash)
	}

	if err := st.DeleteAdmin(ctx, "id-1"); err != nil {
		t.Fatalf("DeleteAdmin: %v", err)
	}
	if _, err := st.GetAdminByID(ctx, "id-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestInviteCanOnlyBeConsumedOnce(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)

	now := Now()
	invite := Invite{
		ID:        "invite-1",
		TokenHash: "hash",
		Email:     "new@example.com",
		CreatedAt: now,
		ExpiresAt: now + 60_000,
	}
	if err := st.CreateInvite(ctx, invite); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	consumed, err := st.ConsumeInvite(ctx, invite.ID, now)
	if err != nil {
		t.Fatalf("ConsumeInvite: %v", err)
	}
	if !consumed {
		t.Fatal("expected the first consume to succeed")
	}

	consumed, err = st.ConsumeInvite(ctx, invite.ID, now)
	if err != nil {
		t.Fatalf("ConsumeInvite: %v", err)
	}
	if consumed {
		t.Fatal("expected the second consume to fail")
	}

	stored, err := st.GetInviteByTokenHash(ctx, "hash")
	if err != nil {
		t.Fatalf("GetInviteByTokenHash: %v", err)
	}
	if stored.State(now) != InviteStateUsed {
		t.Errorf("state = %q, want used", stored.State(now))
	}

	// Expired invites cannot be consumed either.
	expired := Invite{
		ID:        "invite-2",
		TokenHash: "hash-2",
		Email:     "late@example.com",
		CreatedAt: now - 120_000,
		ExpiresAt: now - 1,
	}
	if err := st.CreateInvite(ctx, expired); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	consumed, err = st.ConsumeInvite(ctx, expired.ID, now)
	if err != nil {
		t.Fatalf("ConsumeInvite: %v", err)
	}
	if consumed {
		t.Fatal("expected an expired invite to be rejected")
	}
}

func TestSessionCleanupRemovesStaleSessions(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)

	if _, err := st.CreateAdmin(ctx, "admin-1", "admin@example.com", "hash"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}

	now := Now()
	stale := Session{
		ID:         "stale",
		AdminID:    "admin-1",
		Stage:      StageFull,
		CSRFToken:  "csrf",
		CreatedAt:  now - 7200_000,
		ExpiresAt:  now + 3600_000,
		LastSeenAt: now - 3600_000,
	}
	fresh := Session{
		ID:         "fresh",
		AdminID:    "admin-1",
		Stage:      StageFull,
		CSRFToken:  "csrf",
		CreatedAt:  now,
		ExpiresAt:  now + 3600_000,
		LastSeenAt: now,
	}
	for _, session := range []Session{stale, fresh} {
		if err := st.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}

	if err := st.DeleteStaleSessions(ctx, now, 1_800_000); err != nil {
		t.Fatalf("DeleteStaleSessions: %v", err)
	}

	if _, err := st.GetSession(ctx, "stale"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected the idle session to be removed, got %v", err)
	}
	if _, err := st.GetSession(ctx, "fresh"); err != nil {
		t.Fatalf("expected the fresh session to survive, got %v", err)
	}
}

func TestSubscriptionConflictsAndLanguages(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)

	subscription := Subscription{
		ID:              "sub-1",
		Kind:            KindRSS,
		Label:           "Feed",
		Source:          "https://example.com/feed.xml",
		Webhook:         "main",
		IntervalSeconds: 900,
		Enabled:         true,
	}
	subscription.SetLanguages([]string{"EN", "en", " fr ", ""})
	if got := subscription.Languages; got != "en,fr" {
		t.Fatalf("languages = %q, want en,fr", got)
	}

	if err := st.CreateSubscription(ctx, &subscription); err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}

	duplicate := subscription
	duplicate.ID = "sub-2"
	if err := st.CreateSubscription(ctx, &duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// A different webhook is a different subscription.
	duplicate.ID = "sub-3"
	duplicate.Webhook = "other"
	if err := st.CreateSubscription(ctx, &duplicate); err != nil {
		t.Fatalf("CreateSubscription for another webhook: %v", err)
	}

	watermark := int64(1234)
	if err := st.UpdateSubscriptionPoll(ctx, "sub-1", &watermark, 5678, ""); err != nil {
		t.Fatalf("UpdateSubscriptionPoll: %v", err)
	}

	stored, err := st.GetSubscription(ctx, "sub-1")
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if stored.Watermark == nil || *stored.Watermark != 1234 {
		t.Fatalf("watermark = %v", stored.Watermark)
	}
	if stored.LastCheckedAt == nil || *stored.LastCheckedAt != 5678 {
		t.Fatalf("last checked = %v", stored.LastCheckedAt)
	}
	if len(stored.LanguageList()) != 2 {
		t.Fatalf("languages = %v", stored.LanguageList())
	}
}

func TestAuditLogIsNewestFirstAndPaginated(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)

	for i := range 5 {
		if err := st.InsertAudit(ctx, AuditEntry{
			At:      int64(1000 + i),
			Actor:   "admin@example.com",
			Action:  ActionSubscriptionAdded,
			Target:  "target",
			Details: `{"index":` + string(rune('0'+i)) + `}`,
		}); err != nil {
			t.Fatalf("InsertAudit: %v", err)
		}
	}

	entries, total, err := st.ListAudit(ctx, 2, 0)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].At != 1004 {
		t.Errorf("first entry at = %d, want the newest (1004)", entries[0].At)
	}

	page, _, err := st.ListAudit(ctx, 2, 2)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(page) != 2 || page[0].At != 1002 {
		t.Fatalf("unexpected second page: %+v", page)
	}
}
