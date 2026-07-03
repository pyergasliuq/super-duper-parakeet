// Package config holds all runtime configuration loaded from env / defaults.
//
// Design choices vs the original Python:
//   - All env vars are validated at startup; missing required vars fail fast.
//   - Numeric envs (API_ID, limits) are parsed with explicit int parsing and
//     never silently passed as strings to upstream libraries.
//   - The "loging_id" hardcoded list of admin chat IDs is now ADMIN_IDS,
//     parsed from a comma-separated env var. Backward-compatible default.
package config

import (
        "fmt"
        "os"
        "strconv"
        "strings"
        "time"
)

// Config aggregates every tunable knob. Construct once at startup, pass by
// pointer to every package that needs it. Never mutate after startup.
type Config struct {
        // ── Telegram ───────────────────────────────────────────────────────────
        BotToken  string // main bot token (BOT_TOKEN env, was "TOKEN" in Python)
        APIID     int    // MTProto api_id (was string in Python — bug)
        APIHash   string // MTProto api_hash
        AdminIDs  []int64 // primary devs/admins to receive critical alerts

        // ── Database ───────────────────────────────────────────────────────────
        DBPath string // SQLite path (default: data/users.db)

        // ── Work directory ─────────────────────────────────────────────────────
        WorkDir     string // root working directory for temp files
        MaxWorkGB   float64 // auto-cleanup threshold
        AssetsDir   string // path to embedded/static assets

        // ── Free vs Premium limits ─────────────────────────────────────────────
        FreeMaxFileMB    int
        FreeDelaySec     int
        FreeTimeoutSec   int
        PaidMaxFileMB    int

        // ── Antispam ───────────────────────────────────────────────────────────
        AntispamWindow       int // seconds
        AntispamLimit        int // messages per window
        AntispamBlockSec     int // block duration
        FileAntispamWindow   int
        FileAntispamLimit    int
        FileAntispamBlockSec int

        // ── Queue (semaphores) ─────────────────────────────────────────────────
        FreeWorkers int // concurrent free users
        PaidWorkers int // concurrent paid users

        // ── Subscription plans ─────────────────────────────────────────────────
        Plans []Plan

        // ── Referral ───────────────────────────────────────────────────────────
        RefRewardPct     int   // L1 reward %
        L2RewardTiers    []TierPct
        RefBonusTiers    []TierPct // buyer discount tiers
        FreeRefMilestone int       // 25 → award 1 month Pro

        // ── Trial ──────────────────────────────────────────────────────────────
        TrialStars int
        TrialDays  int

        // ── Default required channels ──────────────────────────────────────────
        DefaultChannels []string

        // ── Logging ────────────────────────────────────────────────────────────
        LogFile   string
        LogLevel  string // debug/info/warn/error
}

// Plan describes one subscription tier shown in /start.
type Plan struct {
        Stars int    // price in Telegram Stars (XTR)
        Days  int    // -1 = forever
        Label string // "1 месяц", "Навсегда", ...
        Emoji string
}

// TierPct is a (threshold, percent) pair used by referral bonus and L2 reward tiers.
type TierPct struct {
        MinRefs int
        Pct     int
}

// Load reads env vars and returns a populated Config or an error describing
// the first missing/invalid required var.
func Load() (*Config, error) {
        cfg := &Config{
                BotToken:              os.Getenv("TOKEN"),
                APIHash:               os.Getenv("API_HASH"),
                DBPath:                envStr("DB_PATH", "data/users.db"),
                WorkDir:               envStr("WORK_DIR", "work"),
                MaxWorkGB:             envFloat("MAX_WORK_GB", 1.5),
                AssetsDir:             envStr("ASSETS_DIR", "assets"),
                FreeMaxFileMB:         envInt("FREE_MAX_FILE_MB", 20),
                FreeDelaySec:          envInt("FREE_DELAY_SEC", 2),
                FreeTimeoutSec:        envInt("FREE_TIMEOUT_SEC", 60),
                PaidMaxFileMB:         envInt("PAID_MAX_FILE_MB", 2048),
                AntispamWindow:        envInt("ANTISPAM_WINDOW", 10),
                AntispamLimit:         envInt("ANTISPAM_LIMIT", 6),
                AntispamBlockSec:      envInt("ANTISPAM_BLOCK_SEC", 45),
                FileAntispamWindow:    envInt("FILE_ANTISPAM_WINDOW", 10),
                FileAntispamLimit:     envInt("FILE_ANTISPAM_LIMIT", 4),
                FileAntispamBlockSec:  envInt("FILE_ANTISPAM_BLOCK_SEC", 30),
                FreeWorkers:           envInt("FREE_WORKERS", 2),
                PaidWorkers:           envInt("PAID_WORKERS", 8),
                RefRewardPct:          envInt("REF_REWARD_PCT", 15),
                FreeRefMilestone:      envInt("FREE_REF_MILESTONE", 25),
                TrialStars:            envInt("TRIAL_STARS", 1),
                TrialDays:             envInt("TRIAL_DAYS", 3),
                LogFile:               envStr("LOG_FILE", "logs/pweper.log"),
                LogLevel:              envStr("LOG_LEVEL", "info"),
                DefaultChannels:       envStrings("DEFAULT_CHANNELS", []string{"@pweper"}),
                L2RewardTiers: []TierPct{
                        {MinRefs: 50, Pct: 4}, {MinRefs: 20, Pct: 3},
                        {MinRefs: 10, Pct: 2}, {MinRefs: 0, Pct: 1},
                },
                RefBonusTiers: []TierPct{
                        {MinRefs: 50, Pct: 25}, {MinRefs: 20, Pct: 20},
                        {MinRefs: 10, Pct: 15}, {MinRefs: 1, Pct: 10},
                },
                Plans: []Plan{
                        {Stars: 50, Days: 14, Label: "2 недели", Emoji: "⚡"},
                        {Stars: 80, Days: 30, Label: "1 месяц", Emoji: "🔥"},
                        {Stars: 200, Days: 90, Label: "3 месяца", Emoji: "💎"},
                        {Stars: 350, Days: 180, Label: "6 месяцев", Emoji: "👑"},
                        {Stars: 600, Days: 365, Label: "1 год", Emoji: "🏆"},
                        {Stars: 1000, Days: -1, Label: "Навсегда", Emoji: "♾️"},
                },
        }

        // API_ID must be int (was string in Python — pyrogram would crash).
        if v := os.Getenv("API_ID"); v != "" {
                n, err := strconv.Atoi(v)
                if err != nil {
                        return nil, fmt.Errorf("API_ID must be integer, got %q: %w", v, err)
                }
                cfg.APIID = n
        }

        // Admin IDs (was hardcoded [2080411409] in Python).
        cfg.AdminIDs = envInt64s("ADMIN_IDS", []int64{2080411409})

        // Required vars.
        if cfg.BotToken == "" {
                return nil, fmt.Errorf("TOKEN env var is required")
        }
        if cfg.APIID == 0 {
                return nil, fmt.Errorf("API_ID env var is required")
        }
        if cfg.APIHash == "" {
                return nil, fmt.Errorf("API_HASH env var is required")
        }

        return cfg, nil
}

// IsAdmin returns true if the user id is in AdminIDs (fast linear scan; list
// is tiny — usually 1-3 entries).
func (c *Config) IsAdmin(userID int64) bool {
        for _, id := range c.AdminIDs {
                if id == userID {
                        return true
                }
        }
        return false
}

// ForeverExpiry is the sentinel "31.12.2099" string used to mark lifetime subs.
// Kept as a constant so we never have a typo.
const ForeverExpiry = "31.12.2099"

// ForeverDate is the parsed time.Time for ForeverExpiry.
func ForeverDate() time.Time {
        t, _ := time.Parse("02.01.2006", ForeverExpiry)
        return t
}

// ── env helpers ───────────────────────────────────────────────────────────

func envStr(k, def string) string {
        if v := os.Getenv(k); v != "" {
                return v
        }
        return def
}

func envInt(k string, def int) int {
        if v := os.Getenv(k); v != "" {
                if n, err := strconv.Atoi(v); err == nil {
                        return n
                }
        }
        return def
}

func envFloat(k string, def float64) float64 {
        if v := os.Getenv(k); v != "" {
                if f, err := strconv.ParseFloat(v, 64); err == nil {
                        return f
                }
        }
        return def
}

func envStrings(k string, def []string) []string {
        if v := os.Getenv(k); v != "" {
                parts := strings.Split(v, ",")
                for i := range parts {
                        parts[i] = strings.TrimSpace(parts[i])
                }
                return parts
        }
        return def
}

func envInt64s(k string, def []int64) []int64 {
        if v := os.Getenv(k); v != "" {
                parts := strings.Split(v, ",")
                out := make([]int64, 0, len(parts))
                for _, p := range parts {
                        p = strings.TrimSpace(p)
                        if n, err := strconv.ParseInt(p, 10, 64); err == nil {
                                out = append(out, n)
                        }
                }
                if len(out) > 0 {
                        return out
                }
        }
        return def
}
