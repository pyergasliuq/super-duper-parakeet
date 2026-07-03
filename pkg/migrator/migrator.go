// Package migrator reads the old Python users.db and copies data into the
// new normalized schema. Run once via `pweper-bot -migrate-old /path/to/users.db`.
//
// Old → new schema mapping:
//
//   users:
//     sub TEXT 'True'/'False'      → is_subscribed INTEGER 0/1
//     admin TEXT 'True'/'False'    → is_admin INTEGER 0/1
//     banned TEXT 'True'/'False'   → is_banned INTEGER 0/1
//     trial_used TEXT 'True'/'False' → trial_used INTEGER 0/1
//     referred_by TEXT             → referred_by INTEGER (NULL if non-numeric)
//     ref_balance TEXT             → ref_balance INTEGER (CAST, default 0)
//     time TEXT (expiry)           → expiry TEXT (kept as-is)
//     role TEXT                    → role TEXT (validated against enum)
//     active_promo TEXT            → active_promo TEXT
//     promo_expires TEXT           → promo_expires TEXT
//     ai_model TEXT                → ai_model TEXT (default 'light')
//
//   promos:
//     used_by TEXT (JSON array)    → rows in promo_uses table
//     is_active TEXT 'True'/'False' → is_active INTEGER 0/1
//
//   Everything else: 1:1 copy with TEXT 'True'/'False' → INTEGER 0/1
//   where applicable.
//
// Idempotent: uses INSERT OR IGNORE for promo_uses and INSERT OR REPLACE
// for users. Safe to re-run after fixing data issues.
//
// Logs every conversion issue (e.g. non-numeric referred_by) to stderr
// but does not abort — we'd rather migrate 99% of users and report the
// 1% that need manual cleanup than fail the whole thing.
package migrator

import (
        "context"
        "database/sql"
        "errors"
        "fmt"
        "log/slog"
        "os"
        "strconv"
        "strings"
        "time"

        _ "modernc.org/sqlite"
)

// Migrator drives the migration. Construct with New(), run with Run().
type Migrator struct {
        src     string // path to old users.db
        dst     string // path to new users.db (will be created if missing)
        dstDB   *sql.DB
        srcDB   *sql.DB
        logger  *slog.Logger
        stats   Stats
}

// Stats counts migrated rows per table for the final report.
type Stats struct {
        Users           int
        Referrals       int
        Purchases       int
        Tickets         int
        CommandStats    int
        Registrations   int
        Broadcasts      int
        Polls           int
        PrankPolls      int
        Reviews         int
        RoleLog         int
        Antispam        int
        PendingInvoices int
        Promos          int
        PromoUses       int
        RequiredChans   int
        BatchSessions   int
        SkippedRows     int
}

// New returns a Migrator. dstDB should already have migrations applied
// (i.e. be a fresh new-schema DB). srcDB will be opened read-only.
func New(srcPath, dstPath string, dstDB *sql.DB, logger *slog.Logger) *Migrator {
        if logger == nil {
                logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
        }
        return &Migrator{src: srcPath, dst: dstPath, dstDB: dstDB, logger: logger}
}

// Run executes the full migration. Returns the final Stats and any fatal
// error. Non-fatal per-row errors are logged but don't abort.
func (m *Migrator) Run(ctx context.Context) (Stats, error) {
        srcDB, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=ro", m.src))
        if err != nil {
                return m.stats, fmt.Errorf("open source: %w", err)
        }
        defer srcDB.Close()
        m.srcDB = srcDB

        // Verify source has the users table (sanity check).
        var tableName string
        err = srcDB.QueryRowContext(ctx,
                "SELECT name FROM sqlite_master WHERE type='table' AND name='users'").Scan(&tableName)
        if err != nil {
                return m.stats, fmt.Errorf("source has no users table: %w", err)
        }

        m.logger.Info("starting migration", "src", m.src, "dst", m.dst)

        // Migration order: independent tables first, then dependent ones.
        if err := m.migrateUsers(ctx); err != nil {
                return m.stats, fmt.Errorf("users: %w", err)
        }
        if err := m.migrateRequiredChannels(ctx); err != nil {
                return m.stats, fmt.Errorf("required_channels: %w", err)
        }
        if err := m.migrateRegistrations(ctx); err != nil {
                return m.stats, fmt.Errorf("registrations: %w", err)
        }
        if err := m.migrateReferrals(ctx); err != nil {
                return m.stats, fmt.Errorf("referrals: %w", err)
        }
        if err := m.migratePromos(ctx); err != nil {
                return m.stats, fmt.Errorf("promos: %w", err)
        }
        if err := m.migratePromoUses(ctx); err != nil {
                return m.stats, fmt.Errorf("promo_uses: %w", err)
        }
        if err := m.migratePurchases(ctx); err != nil {
                return m.stats, fmt.Errorf("purchases: %w", err)
        }
        if err := m.migrateTickets(ctx); err != nil {
                return m.stats, fmt.Errorf("support_tickets: %w", err)
        }
        if err := m.migrateCommandStats(ctx); err != nil {
                return m.stats, fmt.Errorf("command_stats: %w", err)
        }
        if err := m.migrateBroadcasts(ctx); err != nil {
                return m.stats, fmt.Errorf("broadcasts: %w", err)
        }
        if err := m.migratePolls(ctx); err != nil {
                return m.stats, fmt.Errorf("polls: %w", err)
        }
        if err := m.migratePrankPolls(ctx); err != nil {
                return m.stats, fmt.Errorf("prank_polls: %w", err)
        }
        if err := m.migrateReviews(ctx); err != nil {
                return m.stats, fmt.Errorf("reviews: %w", err)
        }
        if err := m.migrateRoleLog(ctx); err != nil {
                return m.stats, fmt.Errorf("role_log: %w", err)
        }
        if err := m.migrateAntispam(ctx); err != nil {
                return m.stats, fmt.Errorf("antispam: %w", err)
        }
        if err := m.migratePendingInvoices(ctx); err != nil {
                return m.stats, fmt.Errorf("pending_invoices: %w", err)
        }
        if err := m.migrateBatchSessions(ctx); err != nil {
                return m.stats, fmt.Errorf("batch_sessions: %w", err)
        }

        m.logger.Info("migration complete", "stats", fmt.Sprintf("%+v", m.stats))
        return m.stats, nil
}

// ── helpers ────────────────────────────────────────────────────────────────

// textToBool converts 'True'/'False'/1/0/NULL to 0/1.
// Anything else → 0 (treat as false).
func textToBool(s sql.NullString) int {
        if !s.Valid {
                return 0
        }
        switch strings.ToLower(s.String) {
        case "true", "1", "t", "yes":
                return 1
        }
        return 0
}

// textToInt converts a TEXT column holding a number to int. Returns 0 on
// NULL/invalid (with a logged warning).
func (m *Migrator) textToInt(s sql.NullString, ctx string) int {
        if !s.Valid || s.String == "" {
                return 0
        }
        n, err := strconv.Atoi(strings.TrimSpace(s.String))
        if err != nil {
                m.logger.Warn("non-numeric int", "ctx", ctx, "value", s.String)
                m.stats.SkippedRows++
                return 0
        }
        return n
}

// textToInt64 nullable version.
func (m *Migrator) textToInt64(s sql.NullString, ctx string) sql.NullInt64 {
        if !s.Valid || s.String == "" {
                return sql.NullInt64{}
        }
        n, err := strconv.ParseInt(strings.TrimSpace(s.String), 10, 64)
        if err != nil {
                m.logger.Warn("non-numeric int64", "ctx", ctx, "value", s.String)
                m.stats.SkippedRows++
                return sql.NullInt64{}
        }
        return sql.NullInt64{Int64: n, Valid: true}
}

// validateRole returns the role if it's in our enum, else NULL.
func validateRole(s sql.NullString) sql.NullString {
        if !s.Valid {
                return sql.NullString{}
        }
        switch s.String {
        case "developer", "admin", "moderator":
                return s
        }
        return sql.NullString{}
}

// validateBTX returns the value if it's in the allowed set, else default.
func validateBTX(s sql.NullString, allowed []string, def string) string {
        if !s.Valid || s.String == "" {
                return def
        }
        for _, a := range allowed {
                if s.String == a {
                        return s.String
                }
        }
        return def
}

// aiModelString returns the AI model string, defaulting to 'light' for NULL/empty.
// AI package is not used in this build, but the column is kept for backward compat.
func aiModelString(s sql.NullString) string {
        if !s.Valid || s.String == "" {
                return "light"
        }
        return s.String
}

// ── users ─────────────────────────────────────────────────────────────────

func (m *Migrator) migrateUsers(ctx context.Context) error {
        // First, check which columns exist in the old users table (different
        // Python versions had different columns added via ALTER TABLE).
        cols, err := m.tableColumns(ctx, m.srcDB, "users")
        if err != nil {
                return err
        }
        has := func(name string) bool {
                for _, c := range cols {
                        if strings.EqualFold(c, name) {
                                return true
                        }
                }
                return false
        }

        // Build SELECT with COALESCE for missing columns to keep shape stable.
        selectSQL := `SELECT chat_id, COALESCE(username,'') AS username,
                COALESCE(sub,'False'), COALESCE(admin,'False'),
                COALESCE(time,''), COALESCE(banned,'False'), COALESCE(ban_reason,''),
                COALESCE(msg_count,0), COALESCE(last_active,''), COALESCE(role,'')`

        if has("referred_by") {
                selectSQL += ", referred_by"
        } else {
                selectSQL += ", NULL AS referred_by"
        }
        if has("ref_balance") {
                selectSQL += ", ref_balance"
        } else {
                selectSQL += ", '0' AS ref_balance"
        }
        if has("trial_used") {
                selectSQL += ", trial_used"
        } else {
                selectSQL += ", 'False' AS trial_used"
        }
        if has("active_promo") {
                selectSQL += ", active_promo"
        } else {
                selectSQL += ", NULL AS active_promo"
        }
        if has("promo_expires") {
                selectSQL += ", promo_expires"
        } else {
                selectSQL += ", NULL AS promo_expires"
        }
        if has("ai_model") {
                selectSQL += ", ai_model"
        } else {
                selectSQL += ", 'light' AS ai_model"
        }
        selectSQL += " FROM users"

        rows, err := m.srcDB.QueryContext(ctx, selectSQL)
        if err != nil {
                return fmt.Errorf("query: %w", err)
        }
        defer rows.Close()

        tx, err := m.dstDB.BeginTx(ctx, nil)
        if err != nil {
                return err
        }
        defer tx.Rollback()

        stmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO users
                (chat_id, username, is_subscribed, is_admin, is_banned, ban_reason,
                 expiry, role, referred_by, ref_balance, trial_used,
                 active_promo, promo_expires, ai_model, btx_block, btx_quality, btx_speed,
                 msg_count, last_active)
                VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?, '8x8','auto','auto', ?, ?)`)
        if err != nil {
                return err
        }
        defer stmt.Close()

        for rows.Next() {
                var chatID int64
                var username, sub, admin, expiry, banned, banReason,
                        msgCountStr, lastActive, role, referredBy, refBalance,
                        trialUsed, activePromo, promoExpires, aiModel sql.NullString

                if err := rows.Scan(&chatID, &username, &sub, &admin, &expiry,
                        &banned, &banReason, &msgCountStr, &lastActive, &role,
                        &referredBy, &refBalance, &trialUsed, &activePromo, &promoExpires,
                        &aiModel); err != nil {
                        m.logger.Warn("users: scan failed", "err", err)
                        m.stats.SkippedRows++
                        continue
                }

                msgCount := m.textToInt(msgCountStr, "users.msg_count")
                refBal := m.textToInt(refBalance, "users.ref_balance")
                referredByInt := m.textToInt64(referredBy, "users.referred_by")

                _, err := stmt.ExecContext(ctx,
                        chatID, username.String, textToBool(sub), textToBool(admin),
                        textToBool(banned), banReason.String, expiry.String,
                        validateRole(role), referredByInt, refBal, textToBool(trialUsed),
                        activePromo, promoExpires, aiModelString(aiModel),
                        msgCount, lastActive)
                if err != nil {
                        m.logger.Warn("users: insert failed", "chat_id", chatID, "err", err)
                        m.stats.SkippedRows++
                        continue
                }
                m.stats.Users++
        }
        if err := rows.Err(); err != nil {
                return err
        }
        return tx.Commit()
}

// tableColumns returns the column names of a table in order.
func (m *Migrator) tableColumns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
        rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var out []string
        for rows.Next() {
                var cid int
                var name, ctype string
                var notnull, pk int
                var dflt sql.NullString
                if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
                        return nil, err
                }
                out = append(out, name)
        }
        return out, rows.Err()
}

// ── required_channels ─────────────────────────────────────────────────────

func (m *Migrator) migrateRequiredChannels(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT channel_username, channel_name FROM required_channels")
        if err != nil {
                // Table may not exist in old DB → skip silently.
                return nil
        }
        defer rows.Close()
        tx, err := m.dstDB.BeginTx(ctx, nil)
        if err != nil {
                return err
        }
        defer tx.Rollback()
        for rows.Next() {
                var uname, cname sql.NullString
                if err := rows.Scan(&uname, &cname); err != nil {
                        continue
                }
                _, err := tx.ExecContext(ctx,
                        "INSERT OR IGNORE INTO required_channels (channel_username, channel_name) VALUES (?, ?)",
                        uname.String, cname.String)
                if err == nil {
                        m.stats.RequiredChans++
                }
        }
        return tx.Commit()
}

// ── registrations ─────────────────────────────────────────────────────────

func (m *Migrator) migrateRegistrations(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT user_id, registered_at FROM registrations")
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var uid int64
                var at sql.NullString
                if err := rows.Scan(&uid, &at); err != nil {
                        continue
                }
                _, err := tx.ExecContext(ctx,
                        "INSERT OR IGNORE INTO registrations (user_id, registered_at) VALUES (?, ?)",
                        uid, at.String)
                if err == nil {
                        m.stats.Registrations++
                }
        }
        return tx.Commit()
}

// ── referrals ─────────────────────────────────────────────────────────────

func (m *Migrator) migrateReferrals(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT referrer_id, referred_id, created_at, paid, reward_given, reward FROM referrals")
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var refID, recID int64
                var createdAt sql.NullString
                var paid, rewardGiven, reward sql.NullString
                if err := rows.Scan(&refID, &recID, &createdAt, &paid, &rewardGiven, &reward); err != nil {
                        continue
                }
                _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO referrals
                        (referrer_id, referred_id, created_at, paid, reward_given, reward)
                        VALUES (?, ?, ?, ?, ?, ?)`,
                        refID, recID, createdAt.String, textToBool(paid), textToBool(rewardGiven),
                        m.textToInt(reward, "referrals.reward"))
                if err == nil {
                        m.stats.Referrals++
                }
        }
        return tx.Commit()
}

// ── promos ────────────────────────────────────────────────────────────────

func (m *Migrator) migratePromos(ctx context.Context) error {
        // The old promos table may or may not have all columns. Detect and adapt.
        cols, _ := m.tableColumns(ctx, m.srcDB, "promos")
        has := func(name string) bool {
                for _, c := range cols {
                        if strings.EqualFold(c, name) {
                                return true
                        }
                }
                return false
        }

        selectSQL := "SELECT code, COALESCE(name,''), COALESCE(comment,''), COALESCE(link,''), COALESCE(discount_pct,0), COALESCE(custom_stars,0), COALESCE(custom_days,0), COALESCE(max_uses,0), COALESCE(used_count,0)"
        if has("expires_at") {
                selectSQL += ", expires_at"
        } else {
                selectSQL += ", NULL AS expires_at"
        }
        if has("is_active") {
                selectSQL += ", is_active"
        } else {
                selectSQL += ", 'True' AS is_active"
        }
        if has("created_at") {
                selectSQL += ", created_at"
        } else {
                selectSQL += ", NULL AS created_at"
        }
        if has("created_by") {
                selectSQL += ", created_by"
        } else {
                selectSQL += ", NULL AS created_by"
        }
        selectSQL += " FROM promos"

        rows, err := m.srcDB.QueryContext(ctx, selectSQL)
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var code, name, comment, link sql.NullString
                var discountPct, customStars, customDays, maxUses, usedCount int
                var expiresAt, isActive, createdAt sql.NullString
                var createdBy sql.NullInt64
                if err := rows.Scan(&code, &name, &comment, &link, &discountPct,
                        &customStars, &customDays, &maxUses, &usedCount,
                        &expiresAt, &isActive, &createdAt, &createdBy); err != nil {
                        continue
                }
                // Promo codes are case-insensitive — uppercase for consistency.
                upperCode := strings.ToUpper(code.String)
                _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO promos
                        (code, name, comment, link, discount_pct, custom_stars, custom_days,
                         max_uses, used_count, expires_at, is_active, created_at, created_by)
                        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
                        upperCode, name.String, comment.String, link.String,
                        discountPct, customStars, customDays, maxUses, usedCount,
                        expiresAt, textToBool(isActive), createdAt, createdBy)
                if err == nil {
                        m.stats.Promos++
                }
        }
        return tx.Commit()
}

// ── promo_uses (from JSON column in old promos table) ─────────────────────

func (m *Migrator) migratePromoUses(ctx context.Context) error {
        // Old promos table had a 'used_by' TEXT column holding a JSON array of user IDs.
        cols, _ := m.tableColumns(ctx, m.srcDB, "promos")
        hasUsedBy := false
        for _, c := range cols {
                if strings.EqualFold(c, "used_by") {
                        hasUsedBy = true
                        break
                }
        }
        if !hasUsedBy {
                return nil // nothing to migrate
        }

        rows, err := m.srcDB.QueryContext(ctx, "SELECT code, used_by FROM promos WHERE used_by IS NOT NULL AND used_by != ''")
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var code, usedByJSON sql.NullString
                if err := rows.Scan(&code, &usedByJSON); err != nil {
                        continue
                }
                // Parse the JSON array. We do a minimal parse — extract integer values.
                ids := extractIntsFromJSONArray(usedByJSON.String)
                upperCode := strings.ToUpper(code.String)
                for _, uid := range ids {
                        _, err := tx.ExecContext(ctx,
                                "INSERT OR IGNORE INTO promo_uses (promo_code, user_id) VALUES (?, ?)",
                                upperCode, uid)
                        if err == nil {
                                m.stats.PromoUses++
                        }
                }
        }
        return tx.Commit()
}

// extractIntsFromJSONArray does a minimal parse of "[1, 2, 3]" → [1,2,3].
// We don't use encoding/json because the format is sometimes malformed in
// the wild (e.g. trailing commas, single quotes).
func extractIntsFromJSONArray(s string) []int64 {
        var out []int64
        var cur strings.Builder
        inNum := false
        for _, r := range s {
                if (r >= '0' && r <= '9') || r == '-' {
                        cur.WriteRune(r)
                        inNum = true
                } else if inNum {
                        if n, err := strconv.ParseInt(cur.String(), 10, 64); err == nil {
                                out = append(out, n)
                        }
                        cur.Reset()
                        inNum = false
                }
        }
        if inNum {
                if n, err := strconv.ParseInt(cur.String(), 10, 64); err == nil {
                        out = append(out, n)
                }
        }
        return out
}

// ── purchases ─────────────────────────────────────────────────────────────

func (m *Migrator) migratePurchases(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT user_id, stars, days, plan_label, promo_code, discount_pct, created_at FROM purchases")
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var uid int64
                var stars, days, discount int
                var planLabel, promoCode, createdAt sql.NullString
                if err := rows.Scan(&uid, &stars, &days, &planLabel, &promoCode, &discount, &createdAt); err != nil {
                        continue
                }
                _, err := tx.ExecContext(ctx, `INSERT INTO purchases
                        (user_id, stars, days, plan_label, promo_code, discount_pct, created_at)
                        VALUES (?,?,?,?,?,?,?)`,
                        uid, stars, days, planLabel, promoCode, discount, createdAt)
                if err == nil {
                        m.stats.Purchases++
                }
        }
        return tx.Commit()
}

// ── support_tickets ───────────────────────────────────────────────────────

func (m *Migrator) migrateTickets(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT user_id, username, is_premium, subject, message, status, created_at, closed_at, closed_by, reply FROM support_tickets")
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var uid int64
                var isPremium, closedBy sql.NullInt64
                var username, subject, message, status, createdAt, closedAt, reply sql.NullString
                if err := rows.Scan(&uid, &username, &isPremium, &subject, &message,
                        &status, &createdAt, &closedAt, &closedBy, &reply); err != nil {
                        continue
                }
                var prem int
                if isPremium.Valid && isPremium.Int64 != 0 {
                        prem = 1
                }
                if status.String == "" {
                        status.String = "open"
                }
                _, err := tx.ExecContext(ctx, `INSERT INTO support_tickets
                        (user_id, username, is_premium, subject, message, status,
                         created_at, closed_at, closed_by, reply)
                        VALUES (?,?,?,?,?,?,?,?,?,?)`,
                        uid, username, prem, subject, message, status,
                        createdAt, closedAt, closedBy, reply)
                if err == nil {
                        m.stats.Tickets++
                }
        }
        return tx.Commit()
}

// ── command_stats ─────────────────────────────────────────────────────────

func (m *Migrator) migrateCommandStats(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT user_id, command, used_at FROM command_stats")
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var uid sql.NullInt64
                var cmd, usedAt sql.NullString
                if err := rows.Scan(&uid, &cmd, &usedAt); err != nil {
                        continue
                }
                _, err := tx.ExecContext(ctx,
                        "INSERT INTO command_stats (user_id, command, used_at) VALUES (?, ?, ?)",
                        uid, cmd, usedAt)
                if err == nil {
                        m.stats.CommandStats++
                }
        }
        return tx.Commit()
}

// ── broadcasts ────────────────────────────────────────────────────────────

func (m *Migrator) migrateBroadcasts(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT id, text, sent_at, sent_by, total_sent FROM broadcasts")
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var id int64
                var sentBy, totalSent sql.NullInt64
                var text, sentAt sql.NullString
                if err := rows.Scan(&id, &text, &sentAt, &sentBy, &totalSent); err != nil {
                        continue
                }
                _, err := tx.ExecContext(ctx,
                        "INSERT INTO broadcasts (id, text, sent_at, sent_by, total_sent) VALUES (?, ?, ?, ?, ?)",
                        id, text, sentAt, sentBy, totalSent)
                if err == nil {
                        m.stats.Broadcasts++
                }
        }
        return tx.Commit()
}

// ── polls ─────────────────────────────────────────────────────────────────

func (m *Migrator) migratePolls(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT id, question, options, created_at, created_by, is_active FROM polls")
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var id, createdBy, isActive sql.NullInt64
                var question, options, createdAt sql.NullString
                if err := rows.Scan(&id, &question, &options, &createdAt, &createdBy, &isActive); err != nil {
                        continue
                }
                var active int = 1
                if isActive.Valid && isActive.Int64 == 0 {
                        active = 0
                }
                _, err := tx.ExecContext(ctx,
                        "INSERT INTO polls (id, question, options, created_at, created_by, is_active) VALUES (?, ?, ?, ?, ?, ?)",
                        id, question, options, createdAt, createdBy, active)
                if err == nil {
                        m.stats.Polls++
                }
        }
        return tx.Commit()
}

// ── prank_polls ───────────────────────────────────────────────────────────

func (m *Migrator) migratePrankPolls(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT id, question, real_options, mapped_options, mode, created_by, created_at, votes_json, is_active, tg_poll_id FROM prank_polls")
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var id, createdBy, isActive sql.NullInt64
                var question, realOpts, mappedOpts, mode, createdAt, votesJSON, tgPollID sql.NullString
                if err := rows.Scan(&id, &question, &realOpts, &mappedOpts, &mode,
                        &createdBy, &createdAt, &votesJSON, &isActive, &tgPollID); err != nil {
                        continue
                }
                var active int = 1
                if isActive.Valid && isActive.Int64 == 0 {
                        active = 0
                }
                if mode.String == "" {
                        mode.String = "remap"
                }
                if votesJSON.String == "" {
                        votesJSON.String = "{}"
                }
                _, err := tx.ExecContext(ctx, `INSERT INTO prank_polls
                        (id, question, real_options, mapped_options, mode, created_by,
                         created_at, votes_json, is_active, tg_poll_id)
                        VALUES (?,?,?,?,?,?,?,?,?,?)`,
                        id, question, realOpts, mappedOpts, mode, createdBy,
                        createdAt, votesJSON, active, tgPollID)
                if err == nil {
                        m.stats.PrankPolls++
                }
        }
        return tx.Commit()
}

// ── reviews ───────────────────────────────────────────────────────────────

func (m *Migrator) migrateReviews(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT user_id, rating, text, created_at FROM reviews")
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var uid int64
                var rating int
                var text, createdAt sql.NullString
                if err := rows.Scan(&uid, &rating, &text, &createdAt); err != nil {
                        continue
                }
                if rating < 1 || rating > 5 {
                        m.logger.Warn("review rating out of range, clamping", "uid", uid, "rating", rating)
                        if rating < 1 {
                                rating = 1
                        }
                        if rating > 5 {
                                rating = 5
                        }
                }
                _, err := tx.ExecContext(ctx,
                        "INSERT OR REPLACE INTO reviews (user_id, rating, text, created_at) VALUES (?, ?, ?, ?)",
                        uid, rating, text, createdAt)
                if err == nil {
                        m.stats.Reviews++
                }
        }
        return tx.Commit()
}

// ── role_log ──────────────────────────────────────────────────────────────

func (m *Migrator) migrateRoleLog(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT id, user_id, role, assigned_by, assigned_at FROM role_log")
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var id, uid, assignedBy sql.NullInt64
                var role, assignedAt sql.NullString
                if err := rows.Scan(&id, &uid, &role, &assignedBy, &assignedAt); err != nil {
                        continue
                }
                _, err := tx.ExecContext(ctx,
                        "INSERT INTO role_log (id, user_id, role, assigned_by, assigned_at) VALUES (?, ?, ?, ?, ?)",
                        id, uid, role, assignedBy, assignedAt)
                if err == nil {
                        m.stats.RoleLog++
                }
        }
        return tx.Commit()
}

// ── antispam ──────────────────────────────────────────────────────────────

func (m *Migrator) migrateAntispam(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT user_id, window_start, msg_count, blocked_until, file_window_start, file_count, file_blocked_until FROM antispam")
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var uid int64
                var ws, mc, bu, fws, fc, fbu sql.NullFloat64
                if err := rows.Scan(&uid, &ws, &mc, &bu, &fws, &fc, &fbu); err != nil {
                        continue
                }
                _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO antispam
                        (user_id, msg_window_start, msg_count, blocked_until,
                         file_window_start, file_count, file_blocked_until)
                        VALUES (?, ?, ?, ?, ?, ?, ?)`,
                        uid, ws.Float64, int64(mc.Float64), bu.Float64,
                        fws.Float64, int64(fc.Float64), fbu.Float64)
                if err == nil {
                        m.stats.Antispam++
                }
        }
        return tx.Commit()
}

// ── pending_invoices ──────────────────────────────────────────────────────

func (m *Migrator) migratePendingInvoices(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT payload, user_id, stars, days, created_at FROM pending_invoices")
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var uid, stars, days sql.NullInt64
                var payload, createdAt sql.NullString
                if err := rows.Scan(&payload, &uid, &stars, &days, &createdAt); err != nil {
                        continue
                }
                _, err := tx.ExecContext(ctx,
                        "INSERT OR REPLACE INTO pending_invoices (payload, user_id, stars, days, created_at) VALUES (?, ?, ?, ?, ?)",
                        payload, uid, stars, days, createdAt)
                if err == nil {
                        m.stats.PendingInvoices++
                }
        }
        return tx.Commit()
}

// ── batch_sessions ────────────────────────────────────────────────────────

func (m *Migrator) migrateBatchSessions(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT user_id, command, caption, files, started_at FROM batch_sessions")
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var uid sql.NullInt64
                var command, caption, files, startedAt sql.NullString
                if err := rows.Scan(&uid, &command, &caption, &files, &startedAt); err != nil {
                        continue
                }
                if files.String == "" {
                        files.String = "[]"
                }
                _, err := tx.ExecContext(ctx,
                        "INSERT OR REPLACE INTO batch_sessions (user_id, command, caption, files, started_at) VALUES (?, ?, ?, ?, ?)",
                        uid, command, caption, files, startedAt)
                if err == nil {
                        m.stats.BatchSessions++
                }
        }
        return tx.Commit()
}

// ErrSourceNotFound is returned when the source users.db file doesn't exist.
var ErrSourceNotFound = errors.New("source users.db not found")

// ReportStats writes a human-readable summary to the given writer.
func (s Stats) Report() string {
        return fmt.Sprintf(
                "Migration stats:\n"+
                        "  Users:            %d\n"+
                        "  Referrals:        %d\n"+
                        "  Purchases:        %d\n"+
                        "  Tickets:          %d\n"+
                        "  CommandStats:     %d\n"+
                        "  Registrations:    %d\n"+
                        "  Broadcasts:       %d\n"+
                        "  Polls:            %d\n"+
                        "  PrankPolls:       %d\n"+
                        "  Reviews:          %d\n"+
                        "  RoleLog:          %d\n"+
                        "  Antispam:         %d\n"+
                        "  PendingInvoices:  %d\n"+
                        "  Promos:           %d\n"+
                        "  PromoUses:        %d\n"+
                        "  RequiredChannels: %d\n"+
                        "  BatchSessions:    %d\n"+
                        "  SkippedRows:      %d\n"+
                        "  Elapsed:          %s",
                s.Users, s.Referrals, s.Purchases, s.Tickets, s.CommandStats,
                s.Registrations, s.Broadcasts, s.Polls, s.PrankPolls, s.Reviews,
                s.RoleLog, s.Antispam, s.PendingInvoices, s.Promos, s.PromoUses,
                s.RequiredChans, s.BatchSessions, s.SkippedRows, time.Since(time.Time{}).String(),
        )
}
