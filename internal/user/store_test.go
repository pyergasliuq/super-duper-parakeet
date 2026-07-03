// Package user — store_test.go
//
// Tests use an in-memory SQLite DB initialized with the real schema
// (via db.Open) so migrations are exercised end-to-end.
package user_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/pweper/bot/internal/db"
	"github.com/pweper/bot/internal/user"
)

// newTestStore returns a fresh in-memory DB + user.Store, ready for tests.
func newTestStore(t *testing.T) (*user.Store, *sql.DB) {
	t.Helper()
	// Use a temp file path so we get WAL + pragmas (modernc :memory: doesn't
	// fully support WAL pragmas, but a temp file works the same in tests).
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	store, err := db.Open(path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return user.NewStore(store.DB()), store.DB()
}

func TestUpsertAndGet(t *testing.T) {
	us, _ := newTestStore(t)
	ctx := context.Background()

	if err := us.Upsert(ctx, 123, "alice"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	u, err := us.Get(ctx, 123)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if u.Username != "alice" {
		t.Errorf("username = %q, want 'alice'", u.Username)
	}
	if u.IsSubscribed {
		t.Errorf("new user should not be subscribed")
	}
	if u.IsBanned {
		t.Errorf("new user should not be banned")
	}
}

func TestGetNotFound(t *testing.T) {
	us, _ := newTestStore(t)
	ctx := context.Background()
	_, err := us.Get(ctx, 999999)
	if err != user.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestBanUnban(t *testing.T) {
	us, _ := newTestStore(t)
	ctx := context.Background()
	_ = us.Upsert(ctx, 1, "alice")

	banned, _, _ := us.CheckBan(ctx, 1)
	if banned {
		t.Errorf("user should not be banned initially")
	}

	if err := us.Ban(ctx, 1, "spam"); err != nil {
		t.Fatalf("Ban: %v", err)
	}
	banned, reason, _ := us.CheckBan(ctx, 1)
	if !banned {
		t.Errorf("user should be banned after Ban()")
	}
	if reason != "spam" {
		t.Errorf("reason = %q, want 'spam'", reason)
	}

	if err := us.Unban(ctx, 1); err != nil {
		t.Fatalf("Unban: %v", err)
	}
	banned, _, _ = us.CheckBan(ctx, 1)
	if banned {
		t.Errorf("user should not be banned after Unban()")
	}
}

func TestGrantSubscription(t *testing.T) {
	us, _ := newTestStore(t)
	ctx := context.Background()
	_ = us.Upsert(ctx, 1, "alice")

	expiry, err := us.GrantSubscription(ctx, 1, 30)
	if err != nil {
		t.Fatalf("GrantSubscription: %v", err)
	}
	want := time.Now().AddDate(0, 0, 30).Format("02.01.2006")
	if expiry != want {
		t.Errorf("expiry = %q, want %q", expiry, want)
	}

	u, _ := us.Get(ctx, 1)
	if !u.IsSubscribed {
		t.Errorf("user should be subscribed")
	}
	if u.IsExpired() {
		t.Errorf("30-day sub should not be expired")
	}
	if u.DaysLeft() < 29 || u.DaysLeft() > 30 {
		t.Errorf("DaysLeft = %d, want 29-30", u.DaysLeft())
	}
}

func TestGrantForeverSubscription(t *testing.T) {
	us, _ := newTestStore(t)
	ctx := context.Background()
	_ = us.Upsert(ctx, 1, "alice")

	expiry, err := us.GrantSubscription(ctx, 1, -1)
	if err != nil {
		t.Fatalf("GrantSubscription: %v", err)
	}
	if expiry != user.ForeverExpiry {
		t.Errorf("expiry = %q, want %q", expiry, user.ForeverExpiry)
	}

	u, _ := us.Get(ctx, 1)
	if u.IsExpired() {
		t.Errorf("forever sub should not expire")
	}
	if u.DaysLeft() != -1 {
		t.Errorf("forever DaysLeft = %d, want -1", u.DaysLeft())
	}
}

func TestIsExpiredWithPastDate(t *testing.T) {
	us, _ := newTestStore(t)
	ctx := context.Background()
	_ = us.Upsert(ctx, 1, "alice")

	// Manually set an expired date.
	_, err := us.DB().ExecContext(ctx,
		"UPDATE users SET is_subscribed = 1, expiry = ? WHERE chat_id = ?",
		"01.01.2000", 1)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	u, _ := us.Get(ctx, 1)
	if !u.IsExpired() {
		t.Errorf("user with 2000 expiry should be expired")
	}
}

func TestSetRole(t *testing.T) {
	us, _ := newTestStore(t)
	ctx := context.Background()
	_ = us.Upsert(ctx, 1, "alice")
	_ = us.Upsert(ctx, 2, "bob")

	if err := us.SetRole(ctx, 1, user.RoleAdmin, 2); err != nil {
		t.Fatalf("SetRole: %v", err)
	}
	u, _ := us.Get(ctx, 1)
	if u.Role != user.RoleAdmin {
		t.Errorf("role = %q, want 'admin'", u.Role)
	}
	if !u.IsAdmin {
		t.Errorf("IsAdmin should be true for admin role")
	}
	if user.RoleLevel(u.Role) != 2 {
		t.Errorf("admin level = %d, want 2", user.RoleLevel(u.Role))
	}

	// Developer > admin.
	_ = us.SetRole(ctx, 1, user.RoleDeveloper, 2)
	u, _ = us.Get(ctx, 1)
	if user.RoleLevel(u.Role) != 3 {
		t.Errorf("developer level = %d, want 3", user.RoleLevel(u.Role))
	}

	// RoleNone clears.
	_ = us.SetRole(ctx, 1, user.RoleNone, 2)
	u, _ = us.Get(ctx, 1)
	if u.Role != user.RoleNone {
		t.Errorf("role = %q, want empty", u.Role)
	}
}

func TestListActiveSubscriptions(t *testing.T) {
	us, _ := newTestStore(t)
	ctx := context.Background()

	// Insert 3 users, 2 subscribed.
	_ = us.Upsert(ctx, 1, "alice")
	_ = us.Upsert(ctx, 2, "bob")
	_ = us.Upsert(ctx, 3, "carol")
	_, _ = us.GrantSubscription(ctx, 1, 30)
	_, _ = us.GrantSubscription(ctx, 2, -1) // forever
	// carol not subscribed

	rows, total, err := us.ListActiveSubscriptions(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListActiveSubscriptions: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	// Non-forever sub should sort first (closest to expire).
	if rows[0].IsForever {
		t.Errorf("first row should be the 30-day sub, not forever")
	}
	if !rows[1].IsForever {
		t.Errorf("second row should be the forever sub")
	}
}

func TestSearchByID(t *testing.T) {
	us, _ := newTestStore(t)
	ctx := context.Background()
	_ = us.Upsert(ctx, 12345, "alice")

	results, err := us.Search(ctx, "12345")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Username != "alice" {
		t.Errorf("username = %q", results[0].Username)
	}
}

func TestSearchByUsername(t *testing.T) {
	us, _ := newTestStore(t)
	ctx := context.Background()
	_ = us.Upsert(ctx, 1, "alice")
	_ = us.Upsert(ctx, 2, "bob")

	// By username without @.
	results, err := us.Search(ctx, "alice")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Username != "alice" {
		t.Errorf("search by username failed: %+v", results)
	}

	// By username with @.
	results, err = us.Search(ctx, "@bob")
	if err != nil {
		t.Fatalf("Search @bob: %v", err)
	}
	if len(results) != 1 || results[0].Username != "bob" {
		t.Errorf("search by @username failed: %+v", results)
	}

	// Case-insensitive.
	results, err = us.Search(ctx, "ALICE")
	if err != nil {
		t.Fatalf("Search ALICE: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("case-insensitive search failed: %+v", results)
	}
}

func TestSearchNotFound(t *testing.T) {
	us, _ := newTestStore(t)
	ctx := context.Background()
	_ = us.Upsert(ctx, 1, "alice")

	results, err := us.Search(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestGetStats(t *testing.T) {
	us, _ := newTestStore(t)
	ctx := context.Background()
	_ = us.Upsert(ctx, 1, "alice")
	_ = us.Upsert(ctx, 2, "bob")
	_, _ = us.GrantSubscription(ctx, 1, 30)
	_ = us.Ban(ctx, 2, "spam")

	stats, err := us.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.Total != 2 {
		t.Errorf("Total = %d, want 2", stats.Total)
	}
	if stats.Paid != 1 {
		t.Errorf("Paid = %d, want 1", stats.Paid)
	}
	if stats.Free != 1 {
		t.Errorf("Free = %d, want 1", stats.Free)
	}
	if stats.Banned != 1 {
		t.Errorf("Banned = %d, want 1", stats.Banned)
	}
}

func TestGetTopUsers(t *testing.T) {
	us, _ := newTestStore(t)
	ctx := context.Background()
	_ = us.Upsert(ctx, 1, "alice")
	_ = us.Upsert(ctx, 2, "bob")
	_ = us.Upsert(ctx, 3, "carol")

	// Bump message counts.
	_, _ = us.DB().ExecContext(ctx,
		"UPDATE users SET msg_count = ? WHERE chat_id = ?", 50, 1)
	_, _ = us.DB().ExecContext(ctx,
		"UPDATE users SET msg_count = ? WHERE chat_id = ?", 100, 2)
	_, _ = us.DB().ExecContext(ctx,
		"UPDATE users SET msg_count = ? WHERE chat_id = ?", 10, 3)

	top, err := us.GetTopUsers(ctx, 3)
	if err != nil {
		t.Fatalf("GetTopUsers: %v", err)
	}
	if len(top) != 3 {
		t.Fatalf("top len = %d, want 3", len(top))
	}
	if top[0].ChatID != 2 || top[0].Count != 100 {
		t.Errorf("top[0] = %+v, want bob/100", top[0])
	}
	if top[1].ChatID != 1 || top[1].Count != 50 {
		t.Errorf("top[1] = %+v, want alice/50", top[1])
	}
}

func TestSetBTXSettings(t *testing.T) {
	us, _ := newTestStore(t)
	ctx := context.Background()
	_ = us.Upsert(ctx, 1, "alice")

	if err := us.SetBTXSettings(ctx, 1, "4x4", "max_quality", "balanced"); err != nil {
		t.Fatalf("SetBTXSettings: %v", err)
	}
	u, _ := us.Get(ctx, 1)
	if u.BTXBlock != "4x4" || u.BTXQuality != "max_quality" || u.BTXSpeed != "balanced" {
		t.Errorf("btx settings not persisted: %+v", u)
	}
}
