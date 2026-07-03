// Package backup creates daily SQLite DB backups and sends them to admin
// via Telegram. Triggered by /admin backup or a daily cron job.
package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// SendDBToAdmin backs up the SQLite DB and sends it to the admin via Telegram.
//
// dbPath is the path to users.db. adminID is the Telegram chat ID to send to.
// caption includes DB size + user count + timestamp.
func SendDBToAdmin(ctx context.Context, bot *tgbotapi.BotAPI, dbPath string, adminIDs []int64, userCount int) error {
	// Copy DB to a temp file (SQLite WAL makes direct send unsafe).
	stamp := time.Now().Format("2006-01-02_15-04")
	tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("pweper-backup-%s.db", stamp))
	if err := copyFile(dbPath, tmpPath); err != nil {
		return fmt.Errorf("copy db: %w", err)
	}
	defer os.Remove(tmpPath)

	// Get file size.
	st, err := os.Stat(tmpPath)
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	sizeKB := st.Size() / 1024

	caption := fmt.Sprintf("💾 <b>Backup users.db</b>\n\n📅 %s\n📊 Пользователей: %d\n📦 Размер: %d КБ",
		stamp, userCount, sizeKB)

	// Send to each admin.
	for _, id := range adminIDs {
		doc := tgbotapi.NewDocument(id, tgbotapi.FileReader{
			Reader: mustOpen(tmpPath),
			Name:   fmt.Sprintf("users-%s.db", stamp),
		})
		doc.Caption = caption
		doc.ParseMode = "HTML"
		if _, err := bot.Send(doc); err != nil {
			return fmt.Errorf("send to %d: %w", id, err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func mustOpen(path string) *os.File {
	f, err := os.Open(path)
	if err != nil {
		return os.NewFile(0, "")
	}
	return f
}
