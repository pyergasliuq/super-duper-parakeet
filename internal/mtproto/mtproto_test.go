// Package mtproto — mtproto_test.go
//
// Tests verify the Config/Client construction without actually connecting
// to Telegram. Real connection tests require a live BOT_TOKEN and are
// skipped in CI.
package mtproto_test

import (
	"log/slog"
	"testing"

	"github.com/pweper/bot/internal/mtproto"
)

func TestNewClient(t *testing.T) {
	cfg := mtproto.Config{
		APIID:      12345,
		APIHash:    "deadbeef",
		BotToken:   "123:abc",
		SessionDir: t.TempDir(),
	}
	c := mtproto.New(cfg, slog.Default())
	if c == nil {
		t.Fatal("New returned nil")
	}
	// Ready channel should not be closed yet.
	select {
	case <-c.Ready():
		t.Error("Ready channel closed before Run")
	default:
		// Expected.
	}
}

func TestNewClientDefaultSessionDir(t *testing.T) {
	cfg := mtproto.Config{
		APIID:    12345,
		APIHash:  "deadbeef",
		BotToken: "123:abc",
		// SessionDir intentionally empty — should default to "data".
	}
	c := mtproto.New(cfg, nil)
	if c == nil {
		t.Fatal("New returned nil")
	}
	// We can't easily check the default value without exposing it, but at
	// least construction should not panic with nil logger.
}

func TestWaitReadyTimeout(t *testing.T) {
	cfg := mtproto.Config{
		APIID:      12345,
		APIHash:    "deadbeef",
		BotToken:   "123:abc",
		SessionDir: t.TempDir(),
	}
	c := mtproto.New(cfg, slog.Default())
	// Without calling Run(), Ready() never closes — WaitReady should time out.
	err := c.WaitReady(0)
	if err == nil {
		t.Error("WaitReady should time out without Run")
	}
}
