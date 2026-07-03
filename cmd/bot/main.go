// Package main is the bot entrypoint.
//
// Usage:
//   pweper-bot            # reads env vars TOKEN, API_ID, API_HASH, ...
//   pweper-bot -migrate   # runs the users.db migration script (one-shot)
//
// All configuration is via env vars (see internal/config/config.go for the
// full list). The bot refuses to start if any required var is missing.
package main

import (
        "context"
        "flag"
        "fmt"
        "log/slog"
        "os"
        "os/signal"
        "sync"
        "syscall"

        "github.com/pweper/bot/internal/bot"
        "github.com/pweper/bot/internal/config"
        "github.com/pweper/bot/internal/db"
        "github.com/pweper/bot/internal/mtproto"
        "github.com/pweper/bot/pkg/migrator"
)

func main() {
        migrateOnly := flag.String("migrate-old", "", "migrate from old Python users.db (TEXT 'True'/'False' columns), then exit")
        migrateV2 := flag.String("migrate-v2", "", "migrate from newer Python users.db (promo_codes table, INTEGER flags), then exit")
        flag.Parse()

        cfg, err := config.Load()
        if err != nil {
                fmt.Fprintf(os.Stderr, "config error: %v\n", err)
                os.Exit(1)
        }

        if *migrateOnly != "" {
                if err := runMigration(cfg, *migrateOnly); err != nil {
                        fmt.Fprintf(os.Stderr, "migration error: %v\n", err)
                        os.Exit(1)
                }
                return
        }
        if *migrateV2 != "" {
                if err := runMigrationV2(cfg, *migrateV2); err != nil {
                        fmt.Fprintf(os.Stderr, "migration error: %v\n", err)
                        os.Exit(1)
                }
                return
        }

        // Signal handling for graceful shutdown.
        ctx, cancel := context.WithCancel(context.Background())
        defer cancel()
        sigCh := make(chan os.Signal, 1)
        signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
        go func() {
                sig := <-sigCh
                fmt.Fprintf(os.Stderr, "received signal %s, shutting down\n", sig)
                cancel()
        }()

        // MTProto client for files >50 MB. DISABLED by default because gotd/td
        // uses utls which can segfault on older CPUs without AVX2.
        // Enable with ENABLE_MTPROTO=1 if you need >50 MB file support.
        if os.Getenv("ENABLE_MTPROTO") == "1" {
                var wg sync.WaitGroup
                wg.Add(1)
                go func() {
                        defer wg.Done()
                        defer func() {
                                if r := recover(); r != nil {
                                        fmt.Fprintf(os.Stderr, "mtproto: panic recovered: %v\n", r)
                                }
                        }()
                        mtProto := mtproto.New(mtproto.Config{
                                APIID:      cfg.APIID,
                                APIHash:    cfg.APIHash,
                                BotToken:   cfg.BotToken,
                                SessionDir: "data",
                        }, slog.Default())
                        if err := mtProto.Run(ctx); err != nil {
                                fmt.Fprintf(os.Stderr, "mtproto: %v\n", err)
                        }
                }()
                fmt.Fprintln(os.Stderr, "MTProto enabled (files >50 MB supported)")
        } else {
                fmt.Fprintln(os.Stderr, "MTProto disabled (set ENABLE_MTPROTO=1 for >50 MB files)")
        }

        // Start the Bot API.
        b, err := bot.New(cfg)
        if err != nil {
                fmt.Fprintf(os.Stderr, "init error: %v\n", err)
                os.Exit(1)
        }
        defer b.Close()

        if err := b.Run(); err != nil {
                fmt.Fprintf(os.Stderr, "run error: %v\n", err)
                os.Exit(1)
        }
        cancel()
}

// runMigration opens both DBs (old Python users.db + new normalized DB),
// applies migrations to the new one, then runs the migrator.
func runMigration(cfg *config.Config, oldPath string) error {
        if _, err := os.Stat(oldPath); err != nil {
                return fmt.Errorf("old db not found: %w", err)
        }

        // Open destination DB (applies migrations automatically).
        store, err := db.Open(cfg.DBPath)
        if err != nil {
                return fmt.Errorf("open dest: %w", err)
        }
        defer store.Close()

        logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
        m := migrator.New(oldPath, cfg.DBPath, store.DB(), logger)
        stats, err := m.Run(nil)
        if err != nil {
                return err
        }
        fmt.Println(stats.Report())
        return nil
}

// runMigrationV2 migrates from the newer Python schema (promo_codes table).
func runMigrationV2(cfg *config.Config, oldPath string) error {
        if _, err := os.Stat(oldPath); err != nil {
                return fmt.Errorf("old db not found: %w", err)
        }
        store, err := db.Open(cfg.DBPath)
        if err != nil {
                return fmt.Errorf("open dest: %w", err)
        }
        defer store.Close()

        logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
        m := migrator.New(oldPath, cfg.DBPath, store.DB(), logger)
        stats, err := m.RunV2(nil)
        if err != nil {
                return err
        }
        fmt.Println(stats.Report())
        return nil
}
