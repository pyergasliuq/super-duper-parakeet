// Package migrator — migrator_test.go
//
// Tests build a fake "old Python" users.db in-memory with the old schema
// (TEXT 'True'/'False' columns etc.) and verify the migration to the new
// normalized schema.
package migrator_test

import (
        "context"
        "database/sql"
        "path/filepath"
        "testing"

        "github.com/pweper/bot/internal/db"
        "github.com/pweper/bot/pkg/migrator"
)

// createOldDB creates a file-based SQLite DB with the OLD Python schema
// (matching main.py lines 1342-1525), seeds it with test data, closes the
// connection, and returns the file path. The file is removed by t.Cleanup.
//
// We must close the writer before the migrator opens it read-only, otherwise
// modernc/sqlite may not flush WAL pages to the main file.
func createOldDB(t *testing.T) string {
        t.Helper()
        path := filepath.Join(t.TempDir(), "old.db")
        dsn := "file:" + path + "?_journal_mode=DELETE&_synchronous=FULL"
        dbOld, err := sql.Open("sqlite", dsn)
        if err != nil {
                t.Fatalf("open old: %v", err)
        }

        // Old schema — matches the Python initialize_database() function.
        schema := `
        CREATE TABLE users (
                chat_id INTEGER PRIMARY KEY,
                username TEXT,
                sub TEXT DEFAULT 'False',
                admin TEXT DEFAULT 'False',
                time TEXT,
                banned TEXT DEFAULT 'False',
                ban_reason TEXT,
                msg_count INTEGER DEFAULT 0,
                last_active TEXT,
                role TEXT,
                referred_by TEXT,
                ref_balance TEXT DEFAULT '0',
                trial_used TEXT DEFAULT 'False',
                active_promo TEXT,
                promo_expires TEXT,
                ai_model TEXT DEFAULT 'light'
        );
        CREATE TABLE required_channels (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                channel_username TEXT UNIQUE NOT NULL,
                channel_name TEXT
        );
        CREATE TABLE registrations (
                user_id INTEGER PRIMARY KEY,
                registered_at TEXT
        );
        CREATE TABLE referrals (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                referrer_id INTEGER NOT NULL,
                referred_id INTEGER NOT NULL UNIQUE,
                created_at TEXT,
                paid INTEGER DEFAULT 0,
                reward_given INTEGER DEFAULT 0,
                reward INTEGER DEFAULT 0
        );
        CREATE TABLE promos (
                code TEXT PRIMARY KEY,
                name TEXT,
                comment TEXT,
                link TEXT,
                discount_pct INTEGER DEFAULT 0,
                custom_stars INTEGER DEFAULT 0,
                custom_days INTEGER DEFAULT 0,
                max_uses INTEGER DEFAULT 0,
                used_count INTEGER DEFAULT 0,
                expires_at TEXT,
                is_active TEXT DEFAULT 'True',
                created_at TEXT,
                created_by INTEGER,
                used_by TEXT DEFAULT '[]'
        );
        CREATE TABLE purchases (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                user_id INTEGER NOT NULL,
                stars INTEGER NOT NULL,
                days INTEGER NOT NULL,
                plan_label TEXT,
                promo_code TEXT,
                discount_pct INTEGER DEFAULT 0,
                created_at TEXT
        );
        CREATE TABLE reviews (
                user_id INTEGER PRIMARY KEY,
                rating INTEGER NOT NULL,
                text TEXT,
                created_at TEXT
        );
        CREATE TABLE antispam (
                user_id INTEGER PRIMARY KEY,
                window_start REAL DEFAULT 0,
                msg_count INTEGER DEFAULT 0,
                blocked_until REAL DEFAULT 0,
                file_window_start REAL DEFAULT 0,
                file_count INTEGER DEFAULT 0,
                file_blocked_until REAL DEFAULT 0
        );
        `
        if _, err := dbOld.Exec(schema); err != nil {
                _ = dbOld.Close()
                t.Fatalf("create old schema: %v", err)
        }

        // Seed test data — mix of "True"/"False" strings and other quirks.
        _, err = dbOld.Exec(`
        INSERT INTO users (chat_id, username, sub, admin, time, banned, ban_reason,
                msg_count, role, referred_by, ref_balance, trial_used, active_promo, ai_model)
        VALUES
                (1, 'alice', 'True', 'True', '31.12.2099', 'False', NULL, 100, 'admin', NULL, '50', 'True', NULL, 'medium'),
                (2, 'bob', 'False', 'False', NULL, 'True', 'spam', 5, NULL, '1', '0', 'False', NULL, 'light'),
                (3, 'carol', 'True', 'False', '15.01.2026', 'False', NULL, 25, 'moderator', '1', '15', 'False', 'SUMMER50', 'best');

        INSERT INTO required_channels (channel_username, channel_name) VALUES
                ('@pweper', '@pweper'),
                ('@nonerai', '@nonerai');

        INSERT INTO registrations (user_id, registered_at) VALUES
                (1, '2025-01-15T10:00:00'),
                (2, '2025-02-20T11:30:00'),
                (3, '2025-03-10T14:15:00');

        INSERT INTO referrals (referrer_id, referred_id, created_at, paid, reward_given, reward)
        VALUES
                (1, 2, '2025-02-20T11:30:00', 1, 1, 12),
                (1, 3, '2025-03-10T14:15:00', 0, 0, 0);

        INSERT INTO promos (code, name, comment, link, discount_pct, max_uses, used_count,
                is_active, used_by)
        VALUES
                ('SUMMER50', 'Summer Sale', '50% off', 'https://t.me/pweper', 50, 100, 2, 'True', '[2, 3]'),
                ('OLDUSER', 'Old user bonus', '', '', 10, 0, 0, 'False', '[]');

        INSERT INTO purchases (user_id, stars, days, plan_label, promo_code, discount_pct, created_at)
        VALUES
                (2, 40, 30, '1 месяц -50%', 'SUMMER50', 50, '2025-02-21T09:00:00');

        INSERT INTO reviews (user_id, rating, text, created_at) VALUES
                (2, 5, 'Отличный бот!', '2025-02-22T12:00:00');

        INSERT INTO antispam (user_id, window_start, msg_count, blocked_until,
                file_window_start, file_count, file_blocked_until)
        VALUES
                (2, 1700000000.0, 3, 0, 1700000000.0, 1, 0);
        `)
        if err != nil {
                _ = dbOld.Close()
                t.Fatalf("seed old data: %v", err)
        }

        // CRITICAL: close before returning so the migrator can open the file
        // read-only. We use _journal_mode=DELETE above so no -wal/-shm files
        // remain.
        if err := dbOld.Close(); err != nil {
                t.Fatalf("close old: %v", err)
        }
        return path
}

// newDestDB creates a fresh new-schema DB (runs migrations).
func newDestDB(t *testing.T) (*sql.DB, func()) {
        t.Helper()
        path := filepath.Join(t.TempDir(), "new.db")
        store, err := db.Open(path)
        if err != nil {
                t.Fatalf("db.Open: %v", err)
        }
        return store.DB(), func() { _ = store.Close() }
}

func TestMigrateUsers(t *testing.T) {
        oldPath := createOldDB(t)
        destDB, cleanup := newDestDB(t)
        defer cleanup()

        m := migrator.New(oldPath, "", destDB, nil)
        stats, err := m.Run(context.Background())
        if err != nil {
                t.Fatalf("Run: %v", err)
        }

        // Verify counts.
        if stats.Users != 3 {
                t.Errorf("Users = %d, want 3", stats.Users)
        }
        if stats.Referrals != 2 {
                t.Errorf("Referrals = %d, want 2", stats.Referrals)
        }
        if stats.Promos != 2 {
                t.Errorf("Promos = %d, want 2", stats.Promos)
        }
        if stats.PromoUses != 2 {
                t.Errorf("PromoUses = %d, want 2 (from SUMMER50 used_by=[2,3])", stats.PromoUses)
        }
        if stats.Registrations != 3 {
                t.Errorf("Registrations = %d, want 3", stats.Registrations)
        }
        if stats.Purchases != 1 {
                t.Errorf("Purchases = %d, want 1", stats.Purchases)
        }
        if stats.Reviews != 1 {
                t.Errorf("Reviews = %d, want 1", stats.Reviews)
        }
        if stats.Antispam != 1 {
                t.Errorf("Antispam = %d, want 1", stats.Antispam)
        }
        if stats.RequiredChans != 2 {
                t.Errorf("RequiredChans = %d, want 2", stats.RequiredChans)
        }

        // Spot-check converted user #1 (alice).
        var isSub, isAdmin, isBanned, trialUsed int
        var refBal int
        var role, expiry string
        err = destDB.QueryRow(`SELECT is_subscribed, is_admin, is_banned, trial_used,
                ref_balance, role, expiry FROM users WHERE chat_id = 1`).
                Scan(&isSub, &isAdmin, &isBanned, &trialUsed, &refBal, &role, &expiry)
        if err != nil {
                t.Fatalf("query alice: %v", err)
        }
        if isSub != 1 || isAdmin != 1 || isBanned != 0 || trialUsed != 1 {
                t.Errorf("alice flags wrong: sub=%d admin=%d banned=%d trial=%d",
                        isSub, isAdmin, isBanned, trialUsed)
        }
        if refBal != 50 {
                t.Errorf("alice ref_balance = %d, want 50 (was TEXT '50')", refBal)
        }
        if role != "admin" {
                t.Errorf("alice role = %q, want 'admin'", role)
        }
        if expiry != "31.12.2099" {
                t.Errorf("alice expiry = %q, want '31.12.2099'", expiry)
        }

        // Spot-check user #2 (bob, banned).
        var banReason string
        err = destDB.QueryRow("SELECT ban_reason, is_banned FROM users WHERE chat_id = 2").
                Scan(&banReason, &isBanned)
        if err != nil {
                t.Fatalf("query bob: %v", err)
        }
        if isBanned != 1 || banReason != "spam" {
                t.Errorf("bob: banned=%d reason=%q, want banned=1 reason='spam'", isBanned, banReason)
        }

        // Spot-check that promo_uses got the JSON array parsed.
        var n int
        err = destDB.QueryRow("SELECT COUNT(*) FROM promo_uses WHERE promo_code = 'SUMMER50'").
                Scan(&n)
        if err != nil {
                t.Fatalf("count promo_uses: %v", err)
        }
        if n != 2 {
                t.Errorf("SUMMER50 promo_uses = %d, want 2", n)
        }

        // Promo codes should be uppercased.
        var code string
        err = destDB.QueryRow("SELECT code FROM promos WHERE code = 'SUMMER50'").Scan(&code)
        if err != nil {
                t.Errorf("expected SUMMER50 to be uppercased in promos: %v", err)
        }
}

func TestMigrateIdempotent(t *testing.T) {
        // Running the migrator twice should not double-insert (we use INSERT OR
        // IGNORE / INSERT OR REPLACE everywhere).
        oldPath := createOldDB(t)
        destDB, cleanup := newDestDB(t)
        defer cleanup()

        m := migrator.New(oldPath, "", destDB, nil)
        if _, err := m.Run(context.Background()); err != nil {
                t.Fatalf("first Run: %v", err)
        }
        // Re-run — should not error and counts should be the same.
        stats2, err := m.Run(context.Background())
        if err != nil {
                t.Fatalf("second Run: %v", err)
        }
        var n int
        _ = destDB.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
        if n != 3 {
                t.Errorf("after 2 runs, users = %d, want 3 (not idempotent)", n)
        }
        _ = stats2
}

func TestMigrateMissingSource(t *testing.T) {
        // Source file doesn't exist → should return error.
        destDB, cleanup := newDestDB(t)
        defer cleanup()
        m := migrator.New("/nonexistent/users.db", "", destDB, nil)
        _, err := m.Run(context.Background())
        if err == nil {
                t.Errorf("expected error for missing source, got nil")
        }
}
