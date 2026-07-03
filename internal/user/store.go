// Package user provides the User domain model and a Store for user persistence.
//
// Bug fixes vs the original Python `update()` function (which was a 30-line
// monster doing too many things):
//   - `update` did: ban check + upsert + subscription check + message gen.
//     Split into Get/Upsert/CheckBan/IsSubscribed/FormatGreeting so each
//     piece can be tested and reused independently.
//   - In Python, `sub` and `admin` and `banned` were TEXT 'True'/'False'
//     columns. String comparisons were fragile. Now INTEGER 0/1.
//   - `referred_by` was TEXT in Python (storing referrer id as string); we
//     store INTEGER directly.
//   - `ref_balance` was TEXT in Python (had to COALESCE+CAST everywhere);
//     now INTEGER.
//   - `expiry` was stored as 'DD.MM.YYYY' string with no validation — we
//     keep the format for UI backward compat but add IsExpired() helper.
package user

import (
        "context"
        "database/sql"
        "errors"
        "fmt"
        "strconv"
        "strings"
        "time"
)

// Role enumerates staff roles. Order matters: developer > admin > moderator.
type Role string

const (
        RoleNone      Role = ""
        RoleModerator Role = "moderator"
        RoleAdmin     Role = "admin"
        RoleDeveloper Role = "developer"
)

// RoleLevel returns the numeric level used for permission checks.
// Higher = more powerful. Unknown roles return 0.
func RoleLevel(r Role) int {
        switch r {
        case RoleDeveloper:
                return 3
        case RoleAdmin:
                return 2
        case RoleModerator:
                return 1
        }
        return 0
}

// User is a single chat/user record. Mirrors the `users` table.
type User struct {
        ChatID       int64
        Username     string
        IsSubscribed bool
        IsAdmin      bool
        IsBanned     bool
        BanReason    string
        Expiry       string // 'DD.MM.YYYY' or '31.12.2099' for forever
        Role         Role
        ReferredBy   sql.NullInt64
        RefBalance   int
        TrialUsed    bool
        ActivePromo  sql.NullString
        PromoExpires sql.NullString
        AIModel      string
        BTXBlock     string
        BTXQuality   string
        BTXSpeed     string
        MsgCount     int
        LastActive   sql.NullString
        CreatedAt    string
}

// IsExpired returns true if the user's subscription has expired.
// Forever subs ('31.12.2099') never expire.
func (u *User) IsExpired() bool {
        if !u.IsSubscribed {
                return true
        }
        if u.Expiry == "" {
                return true
        }
        if u.Expiry == "31.12.2099" {
                return false
        }
        t, err := time.Parse("02.01.2006", u.Expiry)
        if err != nil {
                return true // invalid date = treat as expired
        }
        return time.Now().After(t)
}

// DaysLeft returns whole days until subscription expires. Returns -1 for
// forever subs, 0 for expired.
func (u *User) DaysLeft() int {
        if !u.IsSubscribed || u.Expiry == "" {
                return 0
        }
        if u.Expiry == "31.12.2099" {
                return -1
        }
        t, err := time.Parse("02.01.2006", u.Expiry)
        if err != nil {
                return 0
        }
        d := int(time.Until(t).Hours() / 24)
        if d < 0 {
                return 0
        }
        return d
}

// ForeverExpiry is the sentinel date string used for lifetime subscriptions.
const ForeverExpiry = "31.12.2099"

// FormatExpiry returns a human-friendly expiry string.
// "♾️ бессрочно" for forever, "до <DD.MM.YYYY>" otherwise.
func FormatExpiry(expiry string) string {
        if expiry == ForeverExpiry {
                return "♾️ бессрочно"
        }
        return "до " + expiry
}

// ErrNotFound is returned by Get/queries when no row matches.
var ErrNotFound = errors.New("user not found")

// Store provides all user-related DB operations.
type Store struct{ db *sql.DB }

// NewStore returns a Store backed by the given *sql.DB.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// DB exposes the underlying *sql.DB for packages that need direct access
// (e.g. middleware that needs transactions on the same pool).
func (s *Store) DB() *sql.DB { return s.db }

// Get returns the user or ErrNotFound.
func (s *Store) Get(ctx context.Context, chatID int64) (*User, error) {
        row := s.db.QueryRowContext(ctx, `
                SELECT chat_id, COALESCE(username,''), is_subscribed, is_admin, is_banned,
                       COALESCE(ban_reason,''), COALESCE(expiry,''), role,
                       referred_by, ref_balance, trial_used,
                       active_promo, promo_expires, ai_model,
                       btx_block, btx_quality, btx_speed, msg_count, last_active, created_at
                FROM users WHERE chat_id = ?`, chatID)
        u := &User{}
        var isSub, isAdmin, isBanned, trialUsed int
        var role sql.NullString
        if err := row.Scan(&u.ChatID, &u.Username, &isSub, &isAdmin, &isBanned,
                &u.BanReason, &u.Expiry, &role, &u.ReferredBy, &u.RefBalance, &trialUsed,
                &u.ActivePromo, &u.PromoExpires, &u.AIModel,
                &u.BTXBlock, &u.BTXQuality, &u.BTXSpeed, &u.MsgCount, &u.LastActive, &u.CreatedAt); err != nil {
                if errors.Is(err, sql.ErrNoRows) {
                        return nil, ErrNotFound
                }
                return nil, fmt.Errorf("user.Get: %w", err)
        }
        u.IsSubscribed = isSub == 1
        u.IsAdmin = isAdmin == 1
        u.IsBanned = isBanned == 1
        u.TrialUsed = trialUsed == 1
        if role.Valid {
                u.Role = Role(role.String)
        }
        return u, nil
}

// Upsert inserts a new user or updates an existing one's username/last_active.
// Other fields (subscription, role, ban) are NOT touched on update.
func (s *Store) Upsert(ctx context.Context, chatID int64, username string) error {
        _, err := s.db.ExecContext(ctx, `
                INSERT INTO users (chat_id, username, last_active)
                VALUES (?, ?, datetime('now'))
                ON CONFLICT(chat_id) DO UPDATE SET
                        username   = excluded.username,
                        last_active = datetime('now')`,
                chatID, username)
        if err != nil {
                return fmt.Errorf("user.Upsert: %w", err)
        }
        // Track registration (ignore if exists — first-touch wins).
        _, _ = s.db.ExecContext(ctx,
                "INSERT OR IGNORE INTO registrations (user_id) VALUES (?)", chatID)
        return nil
}

// CheckBan returns (banned, reason) for the user.
func (s *Store) CheckBan(ctx context.Context, chatID int64) (bool, string, error) {
        var banned int
        var reason sql.NullString
        err := s.db.QueryRowContext(ctx,
                "SELECT is_banned, ban_reason FROM users WHERE chat_id = ?", chatID).
                Scan(&banned, &reason)
        if err != nil {
                if errors.Is(err, sql.ErrNoRows) {
                        return false, "", nil
                }
                return false, "", fmt.Errorf("user.CheckBan: %w", err)
        }
        if banned == 0 {
                return false, "", nil
        }
        return true, reason.String, nil
}

// Ban marks a user as banned with the given reason.
func (s *Store) Ban(ctx context.Context, chatID int64, reason string) error {
        _, err := s.db.ExecContext(ctx,
                "UPDATE users SET is_banned = 1, ban_reason = ? WHERE chat_id = ?",
                reason, chatID)
        return err
}

// Unban clears the banned flag.
func (s *Store) Unban(ctx context.Context, chatID int64) error {
        _, err := s.db.ExecContext(ctx,
                "UPDATE users SET is_banned = 0, ban_reason = NULL WHERE chat_id = ?", chatID)
        return err
}

// GrantSubscription activates a subscription for `days` days from now
// (or forever if days == -1). Returns the new expiry string.
func (s *Store) GrantSubscription(ctx context.Context, chatID int64, days int) (string, error) {
        var expiry string
        if days == -1 {
                expiry = ForeverExpiry
        } else {
                expiry = time.Now().AddDate(0, 0, days).Format("02.01.2006")
        }
        _, err := s.db.ExecContext(ctx,
                "UPDATE users SET is_subscribed = 1, expiry = ? WHERE chat_id = ?",
                expiry, chatID)
        if err != nil {
                return "", err
        }
        return expiry, nil
}

// RevokeSubscription disables the subscription.
func (s *Store) RevokeSubscription(ctx context.Context, chatID int64) error {
        _, err := s.db.ExecContext(ctx,
                "UPDATE users SET is_subscribed = 0, expiry = NULL WHERE chat_id = ?", chatID)
        return err
}

// IncMsgCount atomically increments the message counter.
func (s *Store) IncMsgCount(ctx context.Context, chatID int64) error {
        _, err := s.db.ExecContext(ctx,
                "UPDATE users SET msg_count = msg_count + 1 WHERE chat_id = ?", chatID)
        return err
}

// SetRole assigns a role and updates is_admin accordingly.
func (s *Store) SetRole(ctx context.Context, chatID int64, role Role, byID int64) error {
        return s.WithTx(ctx, func(tx *sql.Tx) error {
                var newRole any
                isAdmin := 0
                if role != RoleNone {
                        newRole = string(role)
                        if role == RoleAdmin || role == RoleDeveloper {
                                isAdmin = 1
                        }
                }
                res, err := tx.ExecContext(ctx,
                        "UPDATE users SET role = ?, is_admin = ? WHERE chat_id = ?",
                        newRole, isAdmin, chatID)
                if err != nil {
                        return err
                }
                n, _ := res.RowsAffected()
                if n == 0 {
                        return ErrNotFound
                }
                _, err = tx.ExecContext(ctx,
                        `INSERT INTO role_log (user_id, role, assigned_by) VALUES (?, ?, ?)`,
                        chatID, string(role), byID)
                return err
        })
}

// SetBTXSettings persists the user's BTX quality/speed/block preferences.
func (s *Store) SetBTXSettings(ctx context.Context, chatID int64, block, quality, speed string) error {
        _, err := s.db.ExecContext(ctx,
                "UPDATE users SET btx_block = ?, btx_quality = ?, btx_speed = ? WHERE chat_id = ?",
                block, quality, speed, chatID)
        return err
}

// WithTx runs fn inside a transaction on the user store's DB.
func (s *Store) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
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

// ── Helpers for the "active subscriptions" admin view ────────────────────

// SubscriptionRow is one row in the admin "all active subs" list.
type SubscriptionRow struct {
        ChatID    int64
        Username  string
        Expiry    string
        DaysLeft  int
        IsForever bool
        Plan      string // inferred from expiry distance; "" if unknown
}

// ListActiveSubscriptions returns up to `limit` rows offset by `offset`,
// sorted by expiry ASC (closest to expire first).
func (s *Store) ListActiveSubscriptions(ctx context.Context, limit, offset int) ([]SubscriptionRow, int, error) {
        // Get total count first.
        var total int
        if err := s.db.QueryRowContext(ctx,
                "SELECT COUNT(*) FROM users WHERE is_subscribed = 1").Scan(&total); err != nil {
                return nil, 0, err
        }
        if total == 0 {
                return nil, 0, nil
        }

        rows, err := s.db.QueryContext(ctx, `
                SELECT chat_id, COALESCE(username,''), COALESCE(expiry,'')
                FROM users
                WHERE is_subscribed = 1
                ORDER BY
                        CASE WHEN expiry = '31.12.2099' THEN 1 ELSE 0 END ASC,
                        expiry ASC
                LIMIT ? OFFSET ?`, limit, offset)
        if err != nil {
                return nil, 0, fmt.Errorf("ListActiveSubscriptions: %w", err)
        }
        defer rows.Close()

        out := make([]SubscriptionRow, 0, limit)
        for rows.Next() {
                var r SubscriptionRow
                if err := rows.Scan(&r.ChatID, &r.Username, &r.Expiry); err != nil {
                        return nil, 0, err
                }
                if r.Expiry == ForeverExpiry {
                        r.IsForever = true
                        r.DaysLeft = -1
                } else {
                        if t, err := time.Parse("02.01.2006", r.Expiry); err == nil {
                                d := int(time.Until(t).Hours() / 24)
                                if d < 0 {
                                        d = 0
                                }
                                r.DaysLeft = d
                        }
                }
                out = append(out, r)
        }
        return out, total, rows.Err()
}

// Search performs a simple ID/username lookup used by /find in admin panel.
// `query` may be a numeric chat_id or a username (with or without @).
func (s *Store) Search(ctx context.Context, query string) ([]User, error) {
        query = strings.TrimSpace(query)
        if query == "" {
                return nil, nil
        }
        // Try numeric chat_id first.
        if n, err := strconv.ParseInt(query, 10, 64); err == nil {
                u, err := s.Get(ctx, n)
                if err == nil {
                        return []User{*u}, nil
                }
                if errors.Is(err, ErrNotFound) {
                        return nil, nil
                }
                return nil, err
        }
        // Otherwise treat as username (strip leading @).
        username := strings.TrimPrefix(query, "@")
        rows, err := s.db.QueryContext(ctx, `
                SELECT chat_id, COALESCE(username,''), is_subscribed, is_admin, is_banned,
                       COALESCE(ban_reason,''), COALESCE(expiry,''), role,
                       referred_by, ref_balance, trial_used,
                       active_promo, promo_expires, ai_model,
                       btx_block, btx_quality, btx_speed, msg_count, last_active, created_at
                FROM users WHERE LOWER(username) = LOWER(?) LIMIT 20`, username)
        if err != nil {
                return nil, err
        }
        defer rows.Close()

        var out []User
        for rows.Next() {
                var u User
                var isSub, isAdmin, isBanned, trialUsed int
                var role sql.NullString
                if err := rows.Scan(&u.ChatID, &u.Username, &isSub, &isAdmin, &isBanned,
                        &u.BanReason, &u.Expiry, &role, &u.ReferredBy, &u.RefBalance, &trialUsed,
                        &u.ActivePromo, &u.PromoExpires, &u.AIModel,
                        &u.BTXBlock, &u.BTXQuality, &u.BTXSpeed, &u.MsgCount, &u.LastActive, &u.CreatedAt); err != nil {
                        return nil, err
                }
                u.IsSubscribed = isSub == 1
                u.IsAdmin = isAdmin == 1
                u.IsBanned = isBanned == 1
                u.TrialUsed = trialUsed == 1
                if role.Valid {
                        u.Role = Role(role.String)
                }
                out = append(out, u)
        }
        return out, rows.Err()
}

// Stats holds aggregate counts shown on /admin.
type Stats struct {
        Total    int
        Paid     int
        Free     int
        Banned   int
        Today    int
        WorkGB   float64
}

// GetStats returns aggregate counts for the admin dashboard.
func (s *Store) GetStats(ctx context.Context) (*Stats, error) {
        st := &Stats{}
        queries := []struct {
                sql string
                dst *int
        }{
                {"SELECT COUNT(*) FROM users", &st.Total},
                {"SELECT COUNT(*) FROM users WHERE is_subscribed = 1", &st.Paid},
                {"SELECT COUNT(*) FROM users WHERE is_subscribed = 0", &st.Free},
                {"SELECT COUNT(*) FROM users WHERE is_banned = 1", &st.Banned},
                {`SELECT COUNT(*) FROM users WHERE date(last_active) = date('now')`, &st.Today},
        }
        for _, q := range queries {
                if err := s.db.QueryRowContext(ctx, q.sql).Scan(q.dst); err != nil {
                        return nil, err
                }
        }
        return st, nil
}

// TopUser is one row in the "top active users" leaderboard.
type TopUser struct {
        ChatID   int64
        Username string
        Count    int
}

// GetTopUsers returns the top N users by message count.
func (s *Store) GetTopUsers(ctx context.Context, limit int) ([]TopUser, error) {
        rows, err := s.db.QueryContext(ctx, `
                SELECT chat_id, COALESCE(username,''), msg_count
                FROM users
                WHERE msg_count > 0
                ORDER BY msg_count DESC
                LIMIT ?`, limit)
        if err != nil {
                return nil, err
        }
        defer rows.Close()

        out := make([]TopUser, 0, limit)
        for rows.Next() {
                var t TopUser
                if err := rows.Scan(&t.ChatID, &t.Username, &t.Count); err != nil {
                        return nil, err
                }
                out = append(out, t)
        }
        return out, rows.Err()
}
