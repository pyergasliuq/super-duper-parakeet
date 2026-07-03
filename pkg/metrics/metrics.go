// Package metrics exposes counters and histograms for monitoring.
//
// Used by:
//   - /admin metrics command (in-bot view)
//   - Sentry-style error tracking (internal)
//
// NOT exposed via HTTP — all metrics are viewable via Telegram admin panel.
package metrics

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Counters tracks global bot activity.
type Counters struct {
	MessagesTotal     atomic.Int64
	FilesProcessed    atomic.Int64
	BTXEncoded        atomic.Int64
	BTXDecoded        atomic.Int64
	TXDDecoded        atomic.Int64
	MODDecoded        atomic.Int64
	CacheHits         atomic.Int64
	CacheMisses       atomic.Int64
	ErrorsTotal       atomic.Int64
	BroadcastsSent    atomic.Int64
	StartTime         time.Time
}

// Global is the singleton counters instance.
var Global = &Counters{StartTime: time.Now()}

// ErrorEntry records one error for the admin panel.
type ErrorEntry struct {
	Time     time.Time
	Category string // "btx", "txd", "color", etc.
	Message  string
	UserID   int64
}

// ErrorLog holds the last N errors (ring buffer).
type ErrorLog struct {
	mu      sync.Mutex
	entries []ErrorEntry
	max     int
}

// GlobalErrors is the global error log (last 100 errors).
var GlobalErrors = &ErrorLog{max: 100}

// LogError adds an error to the log.
func LogError(category string, err error, userID int64) {
	if err == nil {
		return
	}
	Global.ErrorsTotal.Add(1)
	GlobalErrors.mu.Lock()
	defer GlobalErrors.mu.Unlock()
	GlobalErrors.entries = append(GlobalErrors.entries, ErrorEntry{
		Time:     time.Now(),
		Category: category,
		Message:  err.Error(),
		UserID:   userID,
	})
	if len(GlobalErrors.entries) > GlobalErrors.max {
		GlobalErrors.entries = GlobalErrors.entries[len(GlobalErrors.entries)-GlobalErrors.max:]
	}
}

// RecentErrors returns the last N errors (newest first).
func RecentErrors(n int) []ErrorEntry {
	GlobalErrors.mu.Lock()
	defer GlobalErrors.mu.Unlock()
	if n > len(GlobalErrors.entries) {
		n = len(GlobalErrors.entries)
	}
	out := make([]ErrorEntry, n)
	for i := 0; i < n; i++ {
		out[i] = GlobalErrors.entries[len(GlobalErrors.entries)-1-i]
	}
	return out
}

// Snapshot returns a human-readable summary of all counters.
func Snapshot() string {
	uptime := time.Since(Global.StartTime).Round(time.Second)
	var sb strings.Builder
	fmt.Fprintf(&sb, "📊 <b>Метрики бота</b>\n\n")
	fmt.Fprintf(&sb, "⏱ Аптайм: <b>%s</b>\n\n", uptime)
	fmt.Fprintf(&sb, "💬 Сообщений: <b>%d</b>\n", Global.MessagesTotal.Load())
	fmt.Fprintf(&sb, "📁 Файлов обработано: <b>%d</b>\n", Global.FilesProcessed.Load())
	fmt.Fprintf(&sb, "🔷 BTX закодировано: <b>%d</b>\n", Global.BTXEncoded.Load())
	fmt.Fprintf(&sb, "🔷 BTX декодировано: <b>%d</b>\n", Global.BTXDecoded.Load())
	fmt.Fprintf(&sb, "🎨 TXD декодировано: <b>%d</b>\n", Global.TXDDecoded.Load())
	fmt.Fprintf(&sb, "🔫 MOD декодировано: <b>%d</b>\n", Global.MODDecoded.Load())
	fmt.Fprintf(&sb, "⚡ Кэш попаданий: <b>%d</b>\n", Global.CacheHits.Load())
	fmt.Fprintf(&sb, "❌ Кэш промахов: <b>%d</b>\n", Global.CacheMisses.Load())
	fmt.Fprintf(&sb, "📢 Рассылок отправлено: <b>%d</b>\n", Global.BroadcastsSent.Load())
	fmt.Fprintf(&sb, "🚨 Ошибок всего: <b>%d</b>\n", Global.ErrorsTotal.Load())
	return sb.String()
}
