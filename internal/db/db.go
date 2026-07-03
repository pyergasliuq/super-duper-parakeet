// Package db wraps modernc.org/sqlite with sane defaults (WAL, busy_timeout)
// and exposes a Store that the rest of the code uses for all persistence.
//
// Improvements over the original Python:
//   - Single *sql.DB with connection pool, not one-conn-per-call. modernc/sqlite
//     supports concurrent readers in WAL mode, and we serialize writers via
//     a RWMutex so we never hit "database is locked".
//   - All DDL lives in versioned migration files (0001_init.sql, 0002_*.sql, ...).
//   - No global monkey-patching of sqlite3.connect — the Python version patched
//     the global sqlite3 module, which broke 3rd-party libraries.
//   - Statements are prepared once and reused (sql.Stmt) for hot paths.
package db

import (
        "context"
        "database/sql"
        "embed"
        "fmt"
        "sync"
        "time"

        _ "modernc.org/sqlite"
)

// Store is the single DB handle used everywhere. Safe for concurrent use.
type Store struct {
        db      *sql.DB
        writeMu sync.Mutex // serializes writers; readers don't need it (WAL)
}

// Open creates the Store, applies pragmas, runs migrations, and returns it.
// dsn is a plain file path like "data/users.db" or ":memory:" for an
// in-memory shared DB (useful for tests).
func Open(dsn string) (*Store, error) {
        // modernc/sqlite uses "?_pragma=..." query params for pragmas; we set them
        // here so every connection picks them up automatically.
        //
        // For ":memory:" DSN we use the shared-cache form so that all connections
        // in the pool see the same database (otherwise each conn gets its own
        // private in-memory DB, which breaks everything).
        var dsnFull string
        if dsn == ":memory:" {
                dsnFull = "file::memory:?cache=shared&_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"
        } else {
                dsnFull = fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)", dsn)
        }

        db, err := sql.Open("sqlite", dsnFull)
        if err != nil {
                return nil, fmt.Errorf("open %s: %w", dsn, err)
        }

        // SQLite handles concurrency well in WAL, but only one writer at a time.
        // We allow many idle conns for parallel reads, but cap max open to keep
        // the writer queue short.
        db.SetMaxOpenConns(16)
        db.SetMaxIdleConns(8)
        db.SetConnMaxLifetime(time.Hour)

        s := &Store{db: db}
        if err := s.migrate(context.Background()); err != nil {
                _ = db.Close()
                return nil, fmt.Errorf("migrate: %w", err)
        }
        return s, nil
}

// Close releases the underlying pool.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the raw *sql.DB for packages that need direct access (e.g. the
// migrator, the admin search). Most code should go through Store methods.
func (s *Store) DB() *sql.DB { return s.db }

// WithTx runs fn inside a write transaction. If fn returns an error the tx is
// rolled back. Acquires writeMu first to avoid SQLITE_BUSY on contended writes.
func (s *Store) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
        s.writeMu.Lock()
        defer s.writeMu.Unlock()

        tx, err := s.db.BeginTx(ctx, nil)
        if err != nil {
                return err
        }
        defer func() {
                if p := recover(); p != nil {
                        _ = tx.Rollback()
                        panic(p)
                }
        }()
        if err := fn(tx); err != nil {
                _ = tx.Rollback()
                return err
        }
        return tx.Commit()
}

// Execer is the minimum interface needed by query helpers; both *sql.DB and
// *sql.Tx satisfy it.
type Execer interface {
        Exec(query string, args ...any) (sql.Result, error)
        Query(query string, args ...any) (*sql.Rows, error)
        QueryRow(query string, args ...any) *sql.Row
        QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// migrate runs every embedded SQL file in order, tracking applied migrations
// in the `schema_migrations` table. Idempotent.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

func (s *Store) migrate(ctx context.Context) error {
        if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
                id INTEGER PRIMARY KEY,
                applied_at TEXT NOT NULL DEFAULT (datetime('now'))
        )`); err != nil {
                return fmt.Errorf("create schema_migrations: %w", err)
        }

        entries, err := migrationsFS.ReadDir("migrations")
        if err != nil {
                return fmt.Errorf("read migrations dir: %w", err)
        }

        for _, e := range entries {
                name := e.Name()
                if e.IsDir() || len(name) < 4 || name[len(name)-4:] != ".sql" {
                        continue
                }
                var applied int
                if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE id = ?", migrationID(name)).Scan(&applied); err != nil {
                        return err
                }
                if applied > 0 {
                        continue
                }
                buf, err := migrationsFS.ReadFile("migrations/" + name)
                if err != nil {
                        return fmt.Errorf("read %s: %w", name, err)
                }
                if _, err := s.db.ExecContext(ctx, string(buf)); err != nil {
                        return fmt.Errorf("apply %s: %w", name, err)
                }
                if _, err := s.db.ExecContext(ctx, "INSERT INTO schema_migrations (id) VALUES (?)", migrationID(name)); err != nil {
                        return err
                }
        }
        return nil
}

// migrationID extracts the leading integer from "0001_init.sql" → 1.
func migrationID(name string) int {
        var n int
        for i := 0; i < len(name) && name[i] >= '0' && name[i] <= '9'; i++ {
                n = n*10 + int(name[i]-'0')
        }
        return n
}
