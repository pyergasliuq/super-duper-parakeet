// Package db — db_test.go
//
// Tests use an in-memory SQLite (":memory:" DSN) so they run fast and don't
// touch the filesystem. modernc.org/sqlite supports ":memory:" out of the box.
package db

import (
        "context"
        "database/sql"
        "fmt"
        "testing"
)

func TestOpenAndMigrate(t *testing.T) {
        store, err := Open(":memory:")
        if err != nil {
                t.Fatalf("Open: %v", err)
        }
        defer store.Close()

        // schema_migrations table should exist and have at least one row (0001_init.sql).
        var n int
        if err := store.db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
                t.Fatalf("query schema_migrations: %v", err)
        }
        if n < 1 {
                t.Fatalf("expected at least 1 migration applied, got %d", n)
        }

        // Verify a few critical tables exist.
        for _, table := range []string{"users", "antispam", "referrals", "promos",
                "purchases", "support_tickets", "registrations"} {
                var name string
                err := store.db.QueryRow(
                        "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
                if err != nil {
                        t.Errorf("table %q missing: %v", table, err)
                }
        }
}

func TestMigrateIdempotent(t *testing.T) {
        store, err := Open(":memory:")
        if err != nil {
                t.Fatalf("Open: %v", err)
        }
        defer store.Close()

        // Re-running migrate should not error and should not re-apply migrations.
        if err := store.migrate(context.Background()); err != nil {
                t.Fatalf("second migrate: %v", err)
        }
        var n int
        if err := store.db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
                t.Fatalf("query: %v", err)
        }
        if n != 1 {
                t.Errorf("expected 1 migration row, got %d (migrate is not idempotent)", n)
        }
}

// TestWithTxCommit verifies that a successful tx is committed.
func TestWithTxCommit(t *testing.T) {
        store, err := Open(":memory:")
        if err != nil {
                t.Fatalf("Open: %v", err)
        }
        defer store.Close()

        ctx := context.Background()
        err = store.WithTx(ctx, func(tx *sql.Tx) error {
                _, err := tx.Exec("INSERT INTO users (chat_id, username) VALUES (?, ?)",
                        int64(1), "alice")
                return err
        })
        if err != nil {
                t.Fatalf("WithTx: %v", err)
        }

        var name string
        err = store.db.QueryRow("SELECT username FROM users WHERE chat_id = 1").Scan(&name)
        if err != nil {
                t.Fatalf("query after commit: %v", err)
        }
        if name != "alice" {
                t.Errorf("expected 'alice', got %q", name)
        }
}

// TestWithTxRollback verifies that a failed tx is rolled back.
func TestWithTxRollback(t *testing.T) {
        store, err := Open(":memory:")
        if err != nil {
                t.Fatalf("Open: %v", err)
        }
        defer store.Close()

        ctx := context.Background()
        err = store.WithTx(ctx, func(tx *sql.Tx) error {
                _, _ = tx.Exec("INSERT INTO users (chat_id, username) VALUES (?, ?)",
                        int64(1), "alice")
                return fmt.Errorf("simulated failure")
        })
        if err == nil {
                t.Fatal("expected error, got nil")
        }

        var n int
        if err := store.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&n); err != nil {
                t.Fatalf("count: %v", err)
        }
        if n != 0 {
                t.Errorf("rollback failed: %d rows present", n)
        }
}

func TestConcurrentWriters(t *testing.T) {
        store, err := Open(":memory:")
        if err != nil {
                t.Fatalf("Open: %v", err)
        }
        defer store.Close()

        // 50 concurrent inserts — must not deadlock or hit "database is locked".
        done := make(chan error, 50)
        for i := 0; i < 50; i++ {
                go func(n int) {
                        _, err := store.db.Exec(
                                "INSERT INTO users (chat_id, username) VALUES (?, ?)",
                                int64(1000+n), fmt.Sprintf("u%d", n))
                        done <- err
                }(i)
        }
        for i := 0; i < 50; i++ {
                if err := <-done; err != nil {
                        t.Errorf("goroutine %d: %v", i, err)
                }
        }
        var n int
        if err := store.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&n); err != nil {
                t.Fatalf("count: %v", err)
        }
        if n != 50 {
                t.Errorf("expected 50 rows, got %d", n)
        }
}

func TestMigrationID(t *testing.T) {
        cases := []struct {
                name string
                want int
        }{
                {"0001_init.sql", 1},
                {"0010_add_indexes.sql", 10},
                {"9999_final.sql", 9999},
                {"no_number.sql", 0},
        }
        for _, tc := range cases {
                got := migrationID(tc.name)
                if got != tc.want {
                        t.Errorf("migrationID(%q) = %d, want %d", tc.name, got, tc.want)
                }
        }
}
