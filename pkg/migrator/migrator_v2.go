// Package migrator — migrator_v2.go
//
// Migration from the NEWER Python users.db format (with promo_codes table
// instead of promos, and promo_uses already as a separate table).
//
// Differences from the original migrator.go:
//   - promos → promo_codes (with uses column instead of used_count)
//   - referrals has no `reward` column
//   - referrals has no `reward_given` column (uses paid only)
//   - groq_rate table (skipped — AI is removed)
//
// All other tables match the old migrator.
package migrator

import (
        "context"
        "database/sql"
        "fmt"
        "strings"
)

// RunV2 migrates from the newer Python schema.
func (m *Migrator) RunV2(ctx context.Context) (Stats, error) {
        srcDB, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=ro", m.src))
        if err != nil {
                return m.stats, fmt.Errorf("open source: %w", err)
        }
        defer srcDB.Close()
        m.srcDB = srcDB

        // Verify source has the users table.
        var tableName string
        err = srcDB.QueryRowContext(ctx,
                "SELECT name FROM sqlite_master WHERE type='table' AND name='users'").Scan(&tableName)
        if err != nil {
                return m.stats, fmt.Errorf("source has no users table: %w", err)
        }

        m.logger.Info("starting V2 migration", "src", m.src, "dst", m.dst)

        // Migration order.
        steps := []struct {
                name string
                fn   func(context.Context) error
        }{
                {"users", m.migrateUsersV2},
                {"required_channels", m.migrateRequiredChannels},
                {"registrations", m.migrateRegistrations},
                {"referrals_v2", m.migrateReferralsV2},
                {"promo_codes", m.migratePromoCodesV2},
                {"promo_uses", m.migratePromoUsesV2},
                {"purchases", m.migratePurchases},
                {"tickets", m.migrateTickets},
                {"command_stats", m.migrateCommandStats},
                {"broadcasts", m.migrateBroadcasts},
                {"polls", m.migratePolls},
                {"prank_polls", m.migratePrankPolls},
                {"reviews", m.migrateReviews},
                {"role_log", m.migrateRoleLog},
                {"antispam", m.migrateAntispam},
                {"pending_invoices", m.migratePendingInvoices},
                {"batch_sessions", m.migrateBatchSessions},
        }
        for _, s := range steps {
                if err := s.fn(ctx); err != nil {
                        m.logger.Warn("migration step failed (continuing)", "step", s.name, "err", err)
                }
        }

        m.logger.Info("V2 migration complete", "stats", fmt.Sprintf("%+v", m.stats))
        return m.stats, nil
}

// migrateUsersV2 — for the newer format, msg_count is TEXT (default '0').
func (m *Migrator) migrateUsersV2(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx, `SELECT
                chat_id, COALESCE(username,''), sub, admin, COALESCE(time,''),
                banned, COALESCE(ban_reason,''), COALESCE(msg_count,'0'),
                COALESCE(last_active,''), COALESCE(role,''), referred_by, ref_balance,
                trial_used, active_promo, promo_expires
                FROM users`)
        if err != nil {
                return err
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
                 active_promo, promo_expires, msg_count, last_active)
                VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
        if err != nil {
                return err
        }
        defer stmt.Close()

        for rows.Next() {
                var chatID int64
                var username, sub, admin, expiry, banned, banReason, msgCountStr,
                        lastActive, role, referredBy, refBalance, trialUsed,
                        activePromo, promoExpires sql.NullString

                if err := rows.Scan(&chatID, &username, &sub, &admin, &expiry,
                        &banned, &banReason, &msgCountStr, &lastActive, &role,
                        &referredBy, &refBalance, &trialUsed, &activePromo, &promoExpires); err != nil {
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
                        activePromo, promoExpires, msgCount, lastActive)
                if err != nil {
                        m.logger.Warn("users: insert failed", "chat_id", chatID, "err", err)
                        m.stats.SkippedRows++
                        continue
                }
                m.stats.Users++
        }
        return tx.Commit()
}

// migrateReferralsV2 — newer format has no `reward` column.
func (m *Migrator) migrateReferralsV2(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT referrer_id, referred_id, created_at, paid, reward_given FROM referrals")
        if err != nil {
                // Old format has reward_given; new format might not.
                rows, err = m.srcDB.QueryContext(ctx,
                        "SELECT referrer_id, referred_id, created_at, paid, 0 FROM referrals")
                if err != nil {
                        return nil
                }
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var refID, recID int64
                var createdAt sql.NullString
                var paid, rewardGiven sql.NullInt64
                if err := rows.Scan(&refID, &recID, &createdAt, &paid, &rewardGiven); err != nil {
                        continue
                }
                _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO referrals
                        (referrer_id, referred_id, created_at, paid, reward_given, reward)
                        VALUES (?, ?, ?, ?, ?, 0)`,
                        refID, recID, createdAt.String,
                        intFromNullInt(paid), intFromNullInt(rewardGiven))
                if err == nil {
                        m.stats.Referrals++
                }
        }
        return tx.Commit()
}

// migratePromoCodesV2 — newer format uses promo_codes table.
func (m *Migrator) migratePromoCodesV2(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx, `SELECT code, COALESCE(name,''),
                COALESCE(comment,''), COALESCE(link,''), COALESCE(discount_pct,0),
                COALESCE(custom_stars,0), COALESCE(custom_days,0), COALESCE(max_uses,0),
                COALESCE(uses,0), COALESCE(expires_at,''), COALESCE(is_active,1),
                COALESCE(created_at,''), COALESCE(created_by, NULL)
                FROM promo_codes`)
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var code, name, comment, link sql.NullString
                var discountPct, customStars, customDays, maxUses, usedCount int
                var expiresAt, createdAt sql.NullString
                var isActive int
                var createdBy sql.NullInt64
                if err := rows.Scan(&code, &name, &comment, &link, &discountPct,
                        &customStars, &customDays, &maxUses, &usedCount,
                        &expiresAt, &isActive, &createdAt, &createdBy); err != nil {
                        continue
                }
                upperCode := strings.ToUpper(code.String)
                _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO promos
                        (code, name, comment, link, discount_pct, custom_stars, custom_days,
                         max_uses, used_count, expires_at, is_active, created_at, created_by)
                        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
                        upperCode, name.String, comment.String, link.String,
                        discountPct, customStars, customDays, maxUses, usedCount,
                        expiresAt, isActive, createdAt, createdBy)
                if err == nil {
                        m.stats.Promos++
                }
        }
        return tx.Commit()
}

// migratePromoUsesV2 — newer format already has promo_uses table.
func (m *Migrator) migratePromoUsesV2(ctx context.Context) error {
        rows, err := m.srcDB.QueryContext(ctx,
                "SELECT code, user_id, COALESCE(used_at,'') FROM promo_uses")
        if err != nil {
                return nil
        }
        defer rows.Close()
        tx, _ := m.dstDB.BeginTx(ctx, nil)
        defer tx.Rollback()
        for rows.Next() {
                var code sql.NullString
                var userID int64
                var usedAt sql.NullString
                if err := rows.Scan(&code, &userID, &usedAt); err != nil {
                        continue
                }
                upperCode := strings.ToUpper(code.String)
                _, err := tx.ExecContext(ctx,
                        "INSERT OR IGNORE INTO promo_uses (promo_code, user_id, used_at) VALUES (?, ?, ?)",
                        upperCode, userID, usedAt)
                if err == nil {
                        m.stats.PromoUses++
                }
        }
        return tx.Commit()
}

// intFromNullInt converts sql.NullInt64 to int (0 if NULL).
func intFromNullInt(n sql.NullInt64) int {
        if !n.Valid {
                return 0
        }
        return int(n.Int64)
}
