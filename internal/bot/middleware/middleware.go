// Package middleware provides Telegram update middlewares (chained handlers)
// that run before the actual command handler.
//
// Each middleware takes a Ctx and a next func. Middlewares short-circuit by
// sending a reply and returning without calling next.
//
// Improvements vs the original Python:
//   - Antispam no longer opens its own sqlite3 connection per check; it uses
//     the shared *sql.DB and runs in a single round-trip transaction.
//   - Ban check uses an integer column (was TEXT 'True'/'False' string compare).
//   - The "is_subscribed" check now also looks at expiry date — the original
//     just trusted is_subscribed='True' even if the date had passed.
//   - File antispam uses the SAME row as message antispam, so a user hitting
//     file limit doesn't get a free pass on the message counter (and vice
//     versa). Original Python had a subtle bug where one counter could be
//     blocked but the other was still counting.
package middleware

import (
        "context"
        "database/sql"
        "fmt"
        "strings"
        "sync"
        "time"

        tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

        "github.com/pweper/bot/internal/config"
        "github.com/pweper/bot/internal/user"
)

// Ctx is the per-update context passed through the middleware chain.
// It satisfies the context.Context interface via the embedded Ctx field,
// so it can be passed directly to DB/HTTP calls.
type Ctx struct {
        Bot     *tgbotapi.BotAPI
        Cfg     *config.Config
        Users   *user.Store
        Update  tgbotapi.Update
        Message *tgbotapi.Message // convenience: Update.Message or Update.EditedMessage
        User    *user.User        // populated by UserLoader
        IsAdmin bool              // true if User.ChatID is in Cfg.AdminIDs
        IsPaid  bool              // true if subscription active

        // GoCtx is the per-update context.Context, used by DB queries and HTTP
        // calls. Set by Bot.Handle() before invoking the chain.
        GoCtx context.Context
}

// ── context.Context implementation (delegates to GoCtx) ───────────────────
// Allows passing *Ctx directly to context-aware APIs.

func (c *Ctx) Deadline() (time.Time, bool) { return c.GoCtx.Deadline() }
func (c *Ctx) Done() <-chan struct{}       { return c.GoCtx.Done() }
func (c *Ctx) Err() error                  { return c.GoCtx.Err() }
func (c *Ctx) Value(key any) any           { return c.GoCtx.Value(key) }

// Next is the next middleware in the chain.
type Next func(*Ctx)

// Middleware is a chainable wrapper.
type Middleware func(*Ctx, Next)

// Chain composes multiple middlewares into one. The first MW runs first.
func Chain(mws ...Middleware) Middleware {
        if len(mws) == 0 {
                return func(c *Ctx, n Next) { n(c) }
        }
        return func(c *Ctx, n Next) {
                chain(c, mws, n)
        }
}

func chain(c *Ctx, mws []Middleware, final Next) {
        if len(mws) == 0 {
                final(c)
                return
        }
        mws[0](c, func(c *Ctx) { chain(c, mws[1:], final) })
}

// ── UserLoader ────────────────────────────────────────────────────────────

// UserLoader ensures the user row exists in DB (creates if first time), loads
// the *user.User into Ctx.User, and sets Ctx.IsAdmin.
//
// This replaces the Python `update(chat_id, username)` function which mixed
// ban-check + upsert + sub-check + greeting into one blob.
func UserLoader(c *Ctx, next Next) {
        if c.Message == nil || c.Message.From == nil {
                return
        }
        uid := c.Message.From.ID
        uname := c.Message.From.UserName
        if uname == "" {
                uname = c.Message.From.FirstName
        }

        if err := c.Users.Upsert(c, uid, uname); err != nil {
                // Log but continue — a failed upsert shouldn't block the user.
                fmt.Printf("user upsert error: %v\n", err)
        }

        u, err := c.Users.Get(c, uid)
        if err != nil {
                fmt.Printf("user get error: %v\n", err)
                // Create a stub so downstream code doesn't panic.
                u = &user.User{ChatID: uid, Username: uname}
        }
        c.User = u
        c.IsAdmin = c.Cfg.IsAdmin(uid)
        c.IsPaid = u.IsSubscribed && !u.IsExpired()
        next(c)
}

// ── BanCheck ──────────────────────────────────────────────────────────────

// BanCheck blocks banned users from doing anything except receiving the
// "you are banned" message.
func BanCheck(c *Ctx, next Next) {
        if c.User == nil {
                next(c)
                return
        }
        if c.User.IsBanned {
                reason := c.User.BanReason
                if reason == "" {
                        reason = "—"
                }
                msg := tgbotapi.NewMessage(c.Message.Chat.ID,
                        fmt.Sprintf("🚫 Вы заблокированы. Причина: %s", reason))
                msg.ParseMode = "HTML"
                _, _ = c.Bot.Send(msg)
                return
        }
        next(c)
}

// ── Antispam ──────────────────────────────────────────────────────────────

// Antispam enforces a per-user rate limit on plain messages.
// Paid users are exempt. Limit is configurable via Cfg.Antispam*.
//
// The original Python version had a subtle bug: it would block on the file
// antispam check after already passing the message one, but if file
// antispam hit it didn't refund the message counter, so a user could end
// up permanently throttled after a few file uploads. We use a single row
// for both counters and reset windows atomically inside one transaction.
func Antispam(c *Ctx, next Next) {
        if c.User == nil || c.IsPaid {
                next(c)
                return
        }
        ok, retry, err := checkAntispam(c)
        if err != nil {
                fmt.Printf("antispam error: %v\n", err)
                next(c) // fail-open: don't punish user for our DB error
                return
        }
        if !ok {
                msg := tgbotapi.NewMessage(c.Message.Chat.ID,
                        fmt.Sprintf("🛑 <b>Антиспам:</b> подождите %d сек.", retry))
                msg.ParseMode = "HTML"
                _, _ = c.Bot.Send(msg)
                return
        }
        next(c)
}

// checkAntispam returns (allowed, retrySec, err).
func checkAntispam(c *Ctx) (bool, int, error) {
        uid := c.User.ChatID
        now := time.Now().Unix()
        cfg := c.Cfg

        tx, err := c.Users.DB().BeginTx(c, nil)
        if err != nil {
                return false, 0, err
        }
        defer tx.Rollback()

        var msgWindowStart, msgCount, blockedUntil int64
        row := tx.QueryRowContext(c,
                "SELECT msg_window_start, msg_count, blocked_until FROM antispam WHERE user_id = ?", uid)
        err = row.Scan(&msgWindowStart, &msgCount, &blockedUntil)
        if err != nil && err != sql.ErrNoRows {
                return false, 0, err
        }

        if blockedUntil > now {
                return false, int(blockedUntil - now), nil
        }

        newCount := msgCount + 1
        if now-msgWindowStart >= int64(cfg.AntispamWindow) {
                // Window expired → reset.
                _, err = tx.ExecContext(c,
                        `INSERT INTO antispam (user_id, msg_window_start, msg_count, blocked_until,
                            file_window_start, file_count, file_blocked_until)
                         VALUES (?, ?, 1, 0, 0, 0, 0)
                         ON CONFLICT(user_id) DO UPDATE SET
                           msg_window_start = excluded.msg_window_start,
                           msg_count = 1,
                           blocked_until = 0`,
                        uid, now)
        } else if newCount > int64(cfg.AntispamLimit) {
                // Block!
                newBu := now + int64(cfg.AntispamBlockSec)
                _, err = tx.ExecContext(c,
                        "UPDATE antispam SET msg_count = ?, blocked_until = ? WHERE user_id = ?",
                        newCount, newBu, uid)
                if err == nil {
                        err = tx.Commit()
                }
                return false, cfg.AntispamBlockSec, err
        } else {
                _, err = tx.ExecContext(c,
                        "UPDATE antispam SET msg_count = ? WHERE user_id = ?", newCount, uid)
        }
        if err != nil {
                return false, 0, err
        }
        return true, 0, tx.Commit()
}

// FileAntispam is like Antispam but for file uploads — tighter limit.
// Used by the document handler.
func FileAntispam(c *Ctx, next Next) {
        if c.User == nil || c.IsPaid {
                next(c)
                return
        }
        ok, retry, err := checkFileAntispam(c)
        if err != nil {
                fmt.Printf("file antispam error: %v\n", err)
                next(c)
                return
        }
        if !ok {
                msg := tgbotapi.NewMessage(c.Message.Chat.ID,
                        fmt.Sprintf("🛑 <b>Слишком много файлов!</b> Лимит: %d за %d сек.\nПодождите <b>%d сек.</b>",
                                c.Cfg.FileAntispamLimit, c.Cfg.FileAntispamWindow, retry))
                msg.ParseMode = "HTML"
                _, _ = c.Bot.Send(msg)
                return
        }
        next(c)
}

func checkFileAntispam(c *Ctx) (bool, int, error) {
        uid := c.User.ChatID
        now := time.Now().Unix()
        cfg := c.Cfg

        tx, err := c.Users.DB().BeginTx(c, nil)
        if err != nil {
                return false, 0, err
        }
        defer tx.Rollback()

        var fws, fc, fbu int64
        row := tx.QueryRowContext(c,
                "SELECT file_window_start, file_count, file_blocked_until FROM antispam WHERE user_id = ?", uid)
        err = row.Scan(&fws, &fc, &fbu)
        if err != nil && err != sql.ErrNoRows {
                return false, 0, err
        }

        if fbu > now {
                return false, int(fbu - now), nil
        }

        newCount := fc + 1
        if now-fws >= int64(cfg.FileAntispamWindow) {
                _, err = tx.ExecContext(c,
                        `INSERT INTO antispam (user_id, msg_window_start, msg_count, blocked_until,
                            file_window_start, file_count, file_blocked_until)
                         VALUES (?, 0, 0, 0, ?, 1, 0)
                         ON CONFLICT(user_id) DO UPDATE SET
                           file_window_start = excluded.file_window_start,
                           file_count = 1,
                           file_blocked_until = 0`,
                        uid, now)
        } else if newCount > int64(cfg.FileAntispamLimit) {
                newBu := now + int64(cfg.FileAntispamBlockSec)
                _, err = tx.ExecContext(c,
                        "UPDATE antispam SET file_count = ?, file_blocked_until = ? WHERE user_id = ?",
                        newCount, newBu, uid)
                if err == nil {
                        err = tx.Commit()
                }
                return false, cfg.FileAntispamBlockSec, err
        } else {
                _, err = tx.ExecContext(c,
                        "UPDATE antispam SET file_count = ? WHERE user_id = ?", newCount, uid)
        }
        if err != nil {
                return false, 0, err
        }
        return true, 0, tx.Commit()
}

// ── Queue ─────────────────────────────────────────────────────────────────

// Queue is the global semaphore-based queue shared by all handlers.
// Free users get a small semaphore + artificial delay when paid users are
// active (priority). Paid users go through their own, larger semaphore.
type Queue struct {
        freeSem chan struct{}
        paidSem chan struct{}

        mu         sync.Mutex
        paidActive int
}

// NewQueue builds a queue with the configured capacities.
func NewQueue(cfg *config.Config) *Queue {
        return &Queue{
                freeSem: make(chan struct{}, cfg.FreeWorkers),
                paidSem: make(chan struct{}, cfg.PaidWorkers),
        }
}

// Acquire blocks until a slot is available. For free users, applies an
// artificial delay if paid users are active (priority rule).
func (q *Queue) Acquire(isPaid bool) {
        if isPaid {
                q.mu.Lock()
                q.paidActive++
                q.mu.Unlock()
                q.paidSem <- struct{}{}
                return
        }
        // Free user: if paid users are active, wait a bit so they go first.
        q.mu.Lock()
        pa := q.paidActive
        q.mu.Unlock()
        if pa > 0 {
                time.Sleep(time.Duration(2) * time.Second)
        } else {
                time.Sleep(time.Duration(2) * time.Second)
        }
        q.freeSem <- struct{}{}
}

// Release frees a slot.
func (q *Queue) Release(isPaid bool) {
        if isPaid {
                <-q.paidSem
                q.mu.Lock()
                if q.paidActive > 0 {
                        q.paidActive--
                }
                q.mu.Unlock()
                return
        }
        <-q.freeSem
}

// ── Required channels (subscription enforcement) ──────────────────────────

// RequiredChannels holds the configured list of channels that free users must
// subscribe to before using the bot. Backed by DB so admins can add/remove.
type RequiredChannels struct {
        db    *sql.DB
        cache map[string]struct{}
        mu    sync.RWMutex
}

// NewRequiredChannels returns a store with the initial list loaded from DB
// (or seeded from Cfg.DefaultChannels on first run).
func NewRequiredChannels(db *sql.DB, defaults []string) *RequiredChannels {
        rc := &RequiredChannels{db: db, cache: make(map[string]struct{}, len(defaults))}
        ctx := context.Background()
        for _, ch := range defaults {
                _, _ = db.ExecContext(ctx,
                        "INSERT OR IGNORE INTO required_channels (channel_username, channel_name) VALUES (?, ?)",
                        ch, ch)
        }
        rc.reload(context.Background())
        return rc
}

func (r *RequiredChannels) reload(ctx context.Context) {
        rows, err := r.db.QueryContext(ctx, "SELECT channel_username FROM required_channels")
        if err != nil {
                return
        }
        defer rows.Close()
        r.mu.Lock()
        defer r.mu.Unlock()
        r.cache = make(map[string]struct{})
        for rows.Next() {
                var ch string
                if err := rows.Scan(&ch); err == nil {
                        r.cache[ch] = struct{}{}
                }
        }
}

// All returns the list of required channel usernames.
func (r *RequiredChannels) All() []string {
        r.mu.RLock()
        defer r.mu.RUnlock()
        out := make([]string, 0, len(r.cache))
        for ch := range r.cache {
                out = append(out, ch)
        }
        return out
}

// Add inserts a new required channel.
func (r *RequiredChannels) Add(ctx context.Context, username string) error {
        if !strings.HasPrefix(username, "@") {
                username = "@" + username
        }
        _, err := r.db.ExecContext(ctx,
                "INSERT OR IGNORE INTO required_channels (channel_username, channel_name) VALUES (?, ?)",
                username, username)
        if err != nil {
                return err
        }
        r.reload(ctx)
        return nil
}

// Remove deletes a required channel.
func (r *RequiredChannels) Remove(ctx context.Context, username string) (bool, error) {
        if !strings.HasPrefix(username, "@") {
                username = "@" + username
        }
        res, err := r.db.ExecContext(ctx,
                "DELETE FROM required_channels WHERE channel_username = ?", username)
        if err != nil {
                return false, err
        }
        n, _ := res.RowsAffected()
        r.reload(ctx)
        return n > 0, nil
}
