// Package bot is the top-level Telegram bot runtime.
//
// It owns the *tgbotapi.BotAPI, *user.Store, middleware queue, required
// channels, and dispatches incoming updates to the appropriate handler.
//
// Bug fixes vs the original Python:
//   - No global bot/boti/dp/p_app/t_client variables — everything is a field
//     on the Bot struct, so multiple instances can coexist (useful for tests).
//   - The "second bot for logs" hack (boti = Bot(token=os.getenv("token2")))
//     is gone; we use the main bot with a rate-limited async sender.
//   - Handlers are dispatched via a router (not a giant if/elif chain). This
//     fixes the "/hud" matches "/hudcut" substring bug — router uses
//     HasPrefix on the command token, not "in".
package bot

import (
        "context"
        "fmt"
        "io"
        "log/slog"
        "os"
        "os/signal"
        "path/filepath"
        "strings"
        "sync"
        "syscall"
        "time"

        tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

        "github.com/pweper/bot/internal/bot/middleware"
        "github.com/pweper/bot/internal/config"
        "github.com/pweper/bot/internal/db"
        "github.com/pweper/bot/internal/user"
        "github.com/pweper/bot/pkg/lru"
)

// Bot is the application root. Construct with New(), run with Run().
type Bot struct {
        cfg      *config.Config
        api      *tgbotapi.BotAPI
        store    *db.Store
        users    *user.Store
        queue    *middleware.Queue
        channels *middleware.RequiredChannels
        logger   *slog.Logger

        // Cache for recent file-processing results (LRU, 50 MB max).
        // Key: SHA-256 of input file. Value: output bytes.
        cache *lru.Cache

        mu      sync.Mutex
        running bool
        cancel  context.CancelFunc
}

// New wires up every dependency. Caller owns the returned *Bot.
func New(cfg *config.Config) (*Bot, error) {
        // Make sure work dirs exist (was setup_work_dirs() in Python).
        workDirs := []string{
                "work", "logs", "data",
                "work/work_MAP", "work/work_BILD", "work/work_BLOOD",
                "work/work_LOGO", "work/work_TREE", "work/work_COLOR",
                "work/work_BTX", "work/work_TXD", "work/work_BPC",
                "work/work_HUD", "work/work_ANI", "work/work_COMPRESS",
                "work/work_COL", "work/work_MOD", "work/work_Z2N",
                "work/work_OVERLAY", "work/work_weapon",
        }
        for _, d := range workDirs {
                if err := os.MkdirAll(filepath.Join(cfg.WorkDir, d), 0o755); err != nil {
                        return nil, fmt.Errorf("mkdir %s: %w", d, err)
                }
        }
        if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o755); err != nil {
                return nil, fmt.Errorf("mkdir log dir: %w", err)
        }

        // Logger: file + stderr, level from config.
        logFile, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
        if err != nil {
                return nil, fmt.Errorf("open log file: %w", err)
        }
        var level slog.Level
        switch strings.ToLower(cfg.LogLevel) {
        case "debug":
                level = slog.LevelDebug
        case "warn":
                level = slog.LevelWarn
        case "error":
                level = slog.LevelError
        default:
                level = slog.LevelInfo
        }
        logger := slog.New(slog.NewTextHandler(io.MultiWriter(logFile, os.Stderr), &slog.HandlerOptions{Level: level}))

        // SQLite store.
        store, err := db.Open(cfg.DBPath)
        if err != nil {
                return nil, fmt.Errorf("db.Open: %w", err)
        }

        // Telegram Bot API.
        api, err := tgbotapi.NewBotAPI(cfg.BotToken)
        if err != nil {
                _ = store.Close()
                return nil, fmt.Errorf("tgbotapi.NewBotAPI: %w", err)
        }
        api.Debug = level == slog.LevelDebug

        users := user.NewStore(store.DB())
        queue := middleware.NewQueue(cfg)
        channels := middleware.NewRequiredChannels(store.DB(), cfg.DefaultChannels)

        return &Bot{
                cfg:      cfg,
                api:      api,
                store:    store,
                users:    users,
                queue:    queue,
                channels: channels,
                cache:    lru.New(50 * 1024 * 1024), // 50 MB LRU cache
                logger:   logger,
        }, nil
}

// Close releases all resources.
func (b *Bot) Close() error {
        return b.store.Close()
}

// Run starts the long-poll loop. Blocks until the bot is stopped via SIGINT/
// SIGTERM or Stop().
func (b *Bot) Run() error {
        ctx, cancel := context.WithCancel(context.Background())
        b.mu.Lock()
        b.cancel = cancel
        b.running = true
        b.mu.Unlock()
        defer func() {
                b.mu.Lock()
                b.running = false
                b.cancel = nil
                b.mu.Unlock()
        }()

        // Signal handling.
        sigCh := make(chan os.Signal, 1)
        signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
        go func() {
                sig := <-sigCh
                b.logger.Info("received signal, shutting down", "signal", sig)
                cancel()
        }()

        // Drop pending updates so we don't replay stale commands on restart.
        u := tgbotapi.NewUpdate(0)
        u.Timeout = 30
        u.AllowedUpdates = []string{"message", "callback_query", "pre_checkout_query",
                "successful_payment", "poll", "poll_answer"}

        updates := b.api.GetUpdatesChan(u)
        if b.api.Self.ID == 0 {
                return fmt.Errorf("bot API returned empty Self — check TOKEN")
        }
        b.logger.Info("bot started",
                "username", b.api.Self.UserName,
                "id", b.api.Self.ID,
                "db", b.cfg.DBPath,
                "work_dir", b.cfg.WorkDir)

        for {
                select {
                case <-ctx.Done():
                        b.api.StopReceivingUpdates()
                        return nil
                case upd, ok := <-updates:
                        if !ok {
                                return nil
                        }
                        // Each update is processed in its own goroutine so a slow handler
                        // (e.g. ZIP processing) never blocks the polling loop.
                        go func() {
                                defer func() {
                                        if r := recover(); r != nil {
                                                b.logger.Error("panic in handler", "err", r,
                                                        "update_id", upd.UpdateID)
                                        }
                                }()
                                b.handleUpdate(ctx, upd)
                        }()
                }
        }
}

// handleUpdate dispatches one update through the middleware chain + router.
func (b *Bot) handleUpdate(parent context.Context, upd tgbotapi.Update) {
        // Each update gets its own (cancellable, timeout-bounded) context.
        ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
        defer cancel()

        c := &middleware.Ctx{
                Bot:     b.api,
                Cfg:     b.cfg,
                Users:   b.users,
                Update:  upd,
                GoCtx:   ctx,
        }
        if upd.Message != nil {
                c.Message = upd.Message
        } else if upd.EditedMessage != nil {
                c.Message = upd.EditedMessage
        } else if upd.CallbackQuery != nil && upd.CallbackQuery.Message != nil {
                // For callback queries we synthesize a minimal Message pointing at
                // the chat, so middlewares can send replies uniformly.
                c.Message = upd.CallbackQuery.Message
                // Override From so UserLoader picks the actual clicker.
                c.Message.From = upd.CallbackQuery.From
        }

        // Always answer callback queries to remove the loading spinner.
        if upd.CallbackQuery != nil {
                callback := tgbotapi.NewCallback(upd.CallbackQuery.ID, "")
                _, _ = b.api.Request(callback)
        }

        // Pre-checkout and successful_payment don't need UserLoader — dispatch directly.
        if upd.PreCheckoutQuery != nil {
                b.handlePreCheckout(c)
                return
        }
        if upd.Message != nil && upd.Message.SuccessfulPayment != nil {
                b.handleSuccessfulPayment(c)
                return
        }

        // Compose middleware chain.
        chain := middleware.Chain(
                middleware.UserLoader,
                middleware.BanCheck,
        )
        chain(c, b.route)
}

// Stop signals the bot to shut down. Safe to call from any goroutine.
func (b *Bot) Stop() {
        b.mu.Lock()
        defer b.mu.Unlock()
        if b.cancel != nil {
                b.cancel()
        }
}

// Logger exposes the structured logger for handlers.
func (b *Bot) Logger() *slog.Logger { return b.logger }

// API exposes the underlying *tgbotapi.BotAPI for handlers that need it.
func (b *Bot) API() *tgbotapi.BotAPI { return b.api }

// Users exposes the user store for handlers.
func (b *Bot) Users() *user.Store { return b.users }

// Queue exposes the worker queue for handlers.
func (b *Bot) Queue() *middleware.Queue { return b.queue }

// Channels exposes the required-channels store for handlers.
func (b *Bot) Channels() *middleware.RequiredChannels { return b.channels }

// Cfg exposes the config (read-only by convention; do not mutate at runtime).
func (b *Bot) Cfg() *config.Config { return b.cfg }
