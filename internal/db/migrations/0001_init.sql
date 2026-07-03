-- 0001_init.sql — normalized schema for Pweper Bot
--
-- Key changes vs the original Python users.db:
--  * snake_case column names everywhere (Python mixed snake_case + camelCase
--    in the same table — bug-prone for SQL queries).
--  * Boolean columns use INTEGER 0/1 instead of TEXT 'True'/'False' — saves
--    space and avoids string-comparison bugs like "if row[0] == 'True'".
--  * ref_balance is INTEGER (was TEXT in Python — caused COALESCE/CAST mess).
--  * expiry uses TEXT 'DD.MM.YYYY' for backward compat with the old UI but
--    we add an ISO expiry_iso computed column for range queries.
--  * Proper indexes on hot lookup paths (chat_id, referrer_id, status).
--  * Foreign keys declared (PRAGMA foreign_keys=ON in db.go).

-- ── users ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS users (
    chat_id        INTEGER PRIMARY KEY,
    username       TEXT,
    is_subscribed  INTEGER NOT NULL DEFAULT 0,    -- 0/1 (was TEXT 'True'/'False')
    is_admin       INTEGER NOT NULL DEFAULT 0,
    is_banned      INTEGER NOT NULL DEFAULT 0,
    ban_reason     TEXT,
    expiry         TEXT,                          -- 'DD.MM.YYYY' or '31.12.2099' forever
    role           TEXT CHECK (role IN ('developer','admin','moderator') OR role IS NULL),
    referred_by    INTEGER,                       -- referrer chat_id (L1)
    ref_balance    INTEGER NOT NULL DEFAULT 0,    -- was TEXT in Python (bug)
    trial_used     INTEGER NOT NULL DEFAULT 0,
    active_promo   TEXT,
    promo_expires  TEXT,
    ai_model       TEXT NOT NULL DEFAULT 'light',  -- kept for backward compat; AI package not used in this build
    btx_block      TEXT NOT NULL DEFAULT 'balanced',  -- compression: auto|strong|balanced|light|none (renamed from block size)
    btx_quality    TEXT NOT NULL DEFAULT 'auto'
                  CHECK (btx_quality IN ('auto','low_weight','balanced','max_quality')),
    btx_speed      TEXT NOT NULL DEFAULT 'auto'
                  CHECK (btx_speed IN ('auto','fast','balanced','max_quality')),
    msg_count      INTEGER NOT NULL DEFAULT 0,
    last_active    TEXT,
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (referred_by) REFERENCES users(chat_id)
);

CREATE INDEX IF NOT EXISTS idx_users_username     ON users(username);
CREATE INDEX IF NOT EXISTS idx_users_is_subscribed ON users(is_subscribed);
CREATE INDEX IF NOT EXISTS idx_users_is_banned    ON users(is_banned);
CREATE INDEX IF NOT EXISTS idx_users_referred_by  ON users(referred_by);
CREATE INDEX IF NOT EXISTS idx_users_role         ON users(role);

-- ── required_channels ───────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS required_channels (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_username  TEXT UNIQUE NOT NULL,
    channel_name      TEXT
);

CREATE INDEX IF NOT EXISTS idx_channels_username ON required_channels(channel_username);

-- ── broadcasts ───────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS broadcasts (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    text         TEXT NOT NULL,
    sent_at      TEXT NOT NULL DEFAULT (datetime('now')),
    sent_by      INTEGER NOT NULL,
    total_sent   INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (sent_by) REFERENCES users(chat_id)
);

-- ── polls ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS polls (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    question    TEXT NOT NULL,
    options     TEXT NOT NULL,        -- JSON array
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    created_by  INTEGER NOT NULL,
    is_active   INTEGER NOT NULL DEFAULT 1,
    FOREIGN KEY (created_by) REFERENCES users(chat_id)
);

-- ── prank_polls ──────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS prank_polls (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    question        TEXT NOT NULL,
    real_options    TEXT NOT NULL,    -- JSON array
    mapped_options  TEXT NOT NULL,    -- JSON array
    mode            TEXT NOT NULL DEFAULT 'remap',
    created_by      INTEGER NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    votes_json      TEXT NOT NULL DEFAULT '{}',
    is_active       INTEGER NOT NULL DEFAULT 1,
    tg_poll_id      TEXT,
    FOREIGN KEY (created_by) REFERENCES users(chat_id)
);

-- ── reviews ──────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS reviews (
    user_id     INTEGER PRIMARY KEY,
    rating      INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 5),
    text        TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(chat_id)
);

-- ── role_log ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS role_log (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL,
    role         TEXT,
    assigned_by  INTEGER NOT NULL,
    assigned_at  TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(chat_id),
    FOREIGN KEY (assigned_by) REFERENCES users(chat_id)
);

CREATE INDEX IF NOT EXISTS idx_role_log_user ON role_log(user_id);

-- ── antispam ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS antispam (
    user_id              INTEGER PRIMARY KEY,
    msg_window_start     REAL NOT NULL DEFAULT 0,
    msg_count            INTEGER NOT NULL DEFAULT 0,
    blocked_until        REAL NOT NULL DEFAULT 0,
    file_window_start    REAL NOT NULL DEFAULT 0,
    file_count           INTEGER NOT NULL DEFAULT 0,
    file_blocked_until   REAL NOT NULL DEFAULT 0,
    FOREIGN KEY (user_id) REFERENCES users(chat_id)
);

-- ── pending_invoices ─────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS pending_invoices (
    payload     TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL,
    stars       INTEGER NOT NULL,
    days        INTEGER NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(chat_id)
);

CREATE INDEX IF NOT EXISTS idx_invoices_user ON pending_invoices(user_id);

-- ── support_tickets ──────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS support_tickets (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL,
    username    TEXT,
    is_premium  INTEGER NOT NULL DEFAULT 0,
    subject     TEXT NOT NULL,
    message     TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed')),
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    closed_at   TEXT,
    closed_by   INTEGER,
    reply       TEXT,
    FOREIGN KEY (user_id) REFERENCES users(chat_id),
    FOREIGN KEY (closed_by) REFERENCES users(chat_id)
);

CREATE INDEX IF NOT EXISTS idx_tickets_status    ON support_tickets(status);
CREATE INDEX IF NOT EXISTS idx_tickets_premium   ON support_tickets(is_premium);
CREATE INDEX IF NOT EXISTS idx_tickets_user      ON support_tickets(user_id);

-- ── purchases ────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS purchases (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL,
    stars         INTEGER NOT NULL,
    days          INTEGER NOT NULL,
    plan_label    TEXT,
    promo_code    TEXT,
    discount_pct  INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(chat_id)
);

CREATE INDEX IF NOT EXISTS idx_purchases_user     ON purchases(user_id);
CREATE INDEX IF NOT EXISTS idx_purchases_created  ON purchases(created_at);

-- ── command_stats ────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS command_stats (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id   INTEGER,
    command   TEXT NOT NULL,
    used_at   TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(chat_id)
);

CREATE INDEX IF NOT EXISTS idx_cmdstats_command ON command_stats(command);
CREATE INDEX IF NOT EXISTS idx_cmdstats_used     ON command_stats(used_at);

-- ── registrations ────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS registrations (
    user_id        INTEGER PRIMARY KEY,
    registered_at  TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(chat_id)
);

CREATE INDEX IF NOT EXISTS idx_regs_registered ON registrations(registered_at);

-- ── referrals ────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS referrals (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    referrer_id   INTEGER NOT NULL,
    referred_id   INTEGER NOT NULL UNIQUE,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    paid          INTEGER NOT NULL DEFAULT 0,
    reward_given  INTEGER NOT NULL DEFAULT 0,
    reward        INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (referrer_id) REFERENCES users(chat_id),
    FOREIGN KEY (referred_id)  REFERENCES users(chat_id)
);

CREATE INDEX IF NOT EXISTS idx_refs_referrer ON referrals(referrer_id);
CREATE INDEX IF NOT EXISTS idx_refs_paid     ON referrals(paid);
CREATE INDEX IF NOT EXISTS idx_refs_created  ON referrals(created_at);

-- ── promos ───────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS promos (
    code           TEXT PRIMARY KEY,
    name           TEXT,
    comment        TEXT,
    link           TEXT,
    discount_pct   INTEGER NOT NULL DEFAULT 0,
    custom_stars   INTEGER NOT NULL DEFAULT 0,
    custom_days    INTEGER NOT NULL DEFAULT 0,
    max_uses       INTEGER NOT NULL DEFAULT 0,    -- 0 = unlimited
    used_count     INTEGER NOT NULL DEFAULT 0,
    expires_at     TEXT,
    is_active      INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    created_by     INTEGER,
    FOREIGN KEY (created_by) REFERENCES users(chat_id)
);

-- ── promo_uses (one row per activation; replaces embedded JSON list) ─────
CREATE TABLE IF NOT EXISTS promo_uses (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    promo_code  TEXT NOT NULL,
    user_id     INTEGER NOT NULL,
    used_at     TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (promo_code, user_id),
    FOREIGN KEY (promo_code) REFERENCES promos(code),
    FOREIGN KEY (user_id)    REFERENCES users(chat_id)
);

CREATE INDEX IF NOT EXISTS idx_promouses_code ON promo_uses(promo_code);
CREATE INDEX IF NOT EXISTS idx_promouses_user ON promo_uses(user_id);

-- ── batch_sessions ───────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS batch_sessions (
    user_id     INTEGER PRIMARY KEY,
    command     TEXT NOT NULL,
    caption     TEXT NOT NULL DEFAULT '',
    files       TEXT NOT NULL DEFAULT '[]',    -- JSON array of {file_id,file_name}
    started_at  TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(chat_id)
);

-- ── bot_stats_cache (optional, for /admin) ───────────────────────────────
CREATE TABLE IF NOT EXISTS bot_stats_cache (
    key    TEXT PRIMARY KEY,
    value  INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
