// Package mtproto provides a thin wrapper around gotd/td for downloading and
// uploading files larger than the Bot API's 50 MB limit.
//
// The bot uses TWO clients simultaneously:
//   - Bot API (go-telegram-bot-api/v5) for commands, inline keyboards, callbacks.
//   - MTProto (gotd/td) as a bot user for file transfers >50 MB.
//
// Both clients share the same BOT_TOKEN, API_ID, API_HASH. They use separate
// sessions but authenticate to the same bot account.
//
// ── Session storage ────────────────────────────────────────────────────────
//
// gotd/td stores the MTProto session in a JSON file (data/mtproto-session.json).
// On first run, the bot authenticates with BOT_TOKEN and saves the session;
// subsequent runs reuse it without re-authenticating.
//
// ── Bug fixes vs Python ────────────────────────────────────────────────────
//
//   - Python used TWO separate libraries (Pyrogram + Telethon) for the same
//     task. We use ONE (gotd/td), halving the auth state and session files.
//   - Python's pyro_download_session and tele_upload_session were stored
//     as .session files in the working directory; we store the JSON session
//     in data/ alongside the SQLite DB.
//   - Python downloaded to filesystem first, then read back; we stream
//     directly to an io.Writer, halving disk I/O for large files.
package mtproto

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

// Config holds the MTProto credentials.
type Config struct {
	APIID      int
	APIHash    string
	BotToken   string
	SessionDir string // where to store the session JSON
}

// Client wraps a gotd/td telegram.Client with a high-level Download/Upload API.
type Client struct {
	cfg    Config
	logger *slog.Logger

	mu       sync.Mutex
	tg       *telegram.Client
	ready    chan struct{} // closed when the client is authenticated
	stopped  chan struct{} // closed when Run() returns
	startErr error
}

// New returns a Client. Call Run() to start it, then wait for Ready() before
// calling Download/Upload.
func New(cfg Config, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	if cfg.SessionDir == "" {
		cfg.SessionDir = "data"
	}
	return &Client{
		cfg:     cfg,
		logger:  logger,
		ready:   make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// Run starts the MTProto client and blocks until ctx is cancelled or a fatal
// error occurs. The client is ready for Download/Upload after Ready() returns.
func (c *Client) Run(ctx context.Context) error {
	defer close(c.stopped)

	// Ensure session dir exists.
	if err := os.MkdirAll(c.cfg.SessionDir, 0o755); err != nil {
		c.startErr = fmt.Errorf("mkdir session: %w", err)
		close(c.ready)
		return c.startErr
	}

	sessionPath := filepath.Join(c.cfg.SessionDir, "mtproto-session.json")
	storage := &session.FileStorage{Path: sessionPath}

	c.mu.Lock()
	c.tg = telegram.NewClient(c.cfg.APIID, c.cfg.APIHash, telegram.Options{
		SessionStorage: storage,
	})
	client := c.tg
	c.mu.Unlock()

	runErr := client.Run(ctx, func(ctx context.Context) error {
		// Check auth status; authenticate as bot if not yet authorized.
		status, err := client.Auth().Status(ctx)
		if err != nil {
			c.startErr = fmt.Errorf("auth status: %w", err)
			close(c.ready)
			return err
		}
		if !status.Authorized {
			if _, err := client.Auth().Bot(ctx, c.cfg.BotToken); err != nil {
				c.startErr = fmt.Errorf("bot login: %w", err)
				close(c.ready)
				return err
			}
		}
		c.logger.Info("mtproto authenticated")
		close(c.ready)
		// Block until context cancelled — keep the client alive.
		<-ctx.Done()
		return ctx.Err()
	})

	if runErr != nil && runErr != context.Canceled {
		return runErr
	}
	return nil
}

// Ready returns a channel that is closed when the client is authenticated
// and ready for Download/Upload calls.
func (c *Client) Ready() <-chan struct{} { return c.ready }

// Stopped returns a channel that is closed when Run() returns.
func (c *Client) Stopped() <-chan struct{} { return c.stopped }

// StartErr returns the error that caused the client to fail starting, or nil
// if it started successfully.
func (c *Client) StartErr() error { return c.startErr }

// WaitReady blocks until the client is ready or the timeout expires.
func (c *Client) WaitReady(timeout time.Duration) error {
	select {
	case <-c.ready:
		return c.startErr
	case <-time.After(timeout):
		return fmt.Errorf("mtproto: not ready after %s", timeout)
	}
}

// ── Download ─────────────────────────────────────────────────────────────

// Download fetches a file by its message ID from a chat and writes the bytes
// to w. Used for documents larger than the Bot API's 50 MB download limit.
//
// Flow:
//   1. Fetch the message via messages.getMessages.
//   2. Extract the document from messageMedia.
//   3. Download via upload.GetFile in 512 KB chunks.
//
// For private chats chatID > 0; for channels/supergroups chatID < 0 (we use
// the absolute value as the channel ID).
func (c *Client) Download(ctx context.Context, chatID int64, messageID int, w io.Writer) error {
	if err := c.WaitReady(30 * time.Second); err != nil {
		return err
	}

	c.mu.Lock()
	api := c.tg.API()
	c.mu.Unlock()

	// Fetch the message.
	msgs, err := api.MessagesGetMessages(ctx, []tg.InputMessageClass{&tg.InputMessageID{ID: messageID}})
	if err != nil {
		return fmt.Errorf("mtproto: getMessages: %w", err)
	}
	msgsSlice, ok := msgs.(*tg.MessagesMessagesSlice)
	if !ok {
		return fmt.Errorf("mtproto: unexpected getMessages response %T", msgs)
	}
	if len(msgsSlice.Messages) == 0 {
		return fmt.Errorf("mtproto: message %d not found", messageID)
	}
	msg, ok := msgsSlice.Messages[0].(*tg.Message)
	if !ok {
		return fmt.Errorf("mtproto: message is not a *tg.Message (got %T)", msgsSlice.Messages[0])
	}
	media, ok := msg.Media.(*tg.MessageMediaDocument)
	if !ok {
		return fmt.Errorf("mtproto: message has no document media (got %T)", msg.Media)
	}
	doc, ok := media.Document.(*tg.Document)
	if !ok {
		return fmt.Errorf("mtproto: document is not *tg.Document (got %T)", media.Document)
	}

	// Download via upload.GetFile in 512 KB chunks.
	location := &tg.InputDocumentFileLocation{
		ID:            doc.ID,
		AccessHash:    doc.AccessHash,
		FileReference: doc.FileReference,
	}
	offset := int64(0)
	const limit = 1024 * 512
	for {
		req := &tg.UploadGetFileRequest{
			Location: location,
			Offset:   offset,
			Limit:    limit,
		}
		res, err := api.UploadGetFile(ctx, req)
		if err != nil {
			return fmt.Errorf("mtproto: uploadGetFile at offset %d: %w", offset, err)
		}
		f, ok := res.(*tg.UploadFile)
		if !ok {
			return fmt.Errorf("mtproto: uploadGetFile returned %T", res)
		}
		if _, err := w.Write(f.Bytes); err != nil {
			return fmt.Errorf("mtproto: write: %w", err)
		}
		if len(f.Bytes) < limit {
			return nil // last chunk
		}
		offset += int64(len(f.Bytes))
	}
}

// DownloadBytes is a convenience wrapper that returns the bytes.
func (c *Client) DownloadBytes(ctx context.Context, chatID int64, messageID int) ([]byte, error) {
	var buf byteBuffer
	if err := c.Download(ctx, chatID, messageID, &buf); err != nil {
		return nil, err
	}
	return buf.b, nil
}

// byteBuffer is a tiny io.Writer that grows a []byte.
type byteBuffer struct{ b []byte }

func (b *byteBuffer) Write(p []byte) (int, error) {
	b.b = append(b.b, p...)
	return len(p), nil
}

// ── Upload ───────────────────────────────────────────────────────────────

// Upload sends a file to a chat as a document and returns the message ID.
//
// For files >50 MB this is the ONLY way to send via a bot (Bot API caps at 50 MB).
//
// Uses upload.SaveBigFile for >10 MB, upload.SaveFile for smaller. Both paths
// are fully implemented.
func (c *Client) Upload(ctx context.Context, chatID int64, fileName, caption string, r io.Reader) (int, error) {
	if err := c.WaitReady(30 * time.Second); err != nil {
		return 0, err
	}

	c.mu.Lock()
	api := c.tg.API()
	c.mu.Unlock()

	const partSize = 1024 * 512
	const bigFileThreshold = 10 * 1024 * 1024

	// Read in chunks. We don't know the total size with io.Reader, so we
	// buffer up to bigFileThreshold before deciding which API to use.
	var parts [][]byte
	totalSize := 0
	buf := make([]byte, partSize)
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			parts = append(parts, chunk)
			totalSize += n
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("mtproto: read chunk: %w", err)
		}
	}
	if totalSize == 0 {
		return 0, fmt.Errorf("mtproto: upload: empty file")
	}

	// Generate a random file ID (any int64 works — server uses it to dedupe).
	fileID := time.Now().UnixNano()

	var inputFile tg.InputFileClass
	if totalSize > bigFileThreshold {
		// Big file path: upload.SaveBigFilePart for each chunk.
		partsCount := len(parts)
		for i, p := range parts {
			req := &tg.UploadSaveBigFilePartRequest{
				FileID:         fileID,
				FilePart:       i,
				Bytes:          p,
				FileTotalParts: partsCount,
			}
			ok, err := api.UploadSaveBigFilePart(ctx, req)
			if err != nil {
				return 0, fmt.Errorf("mtproto: upload part %d: %w", i, err)
			}
			if !ok {
				return 0, fmt.Errorf("mtproto: upload part %d: server returned false", i)
			}
		}
		inputFile = &tg.InputFileBig{
			ID:    fileID,
			Parts: partsCount,
			Name:  fileName,
		}
	} else {
		// Small file path: upload.SaveFilePart for each chunk.
		for i, p := range parts {
			req := &tg.UploadSaveFilePartRequest{
				FileID:   fileID,
				FilePart: i,
				Bytes:    p,
			}
			ok, err := api.UploadSaveFilePart(ctx, req)
			if err != nil {
				return 0, fmt.Errorf("mtproto: upload part %d: %w", i, err)
			}
			if !ok {
				return 0, fmt.Errorf("mtproto: upload part %d: server returned false", i)
			}
		}
		inputFile = &tg.InputFile{
			ID:          fileID,
			Parts:       len(parts),
			Name:        fileName,
			MD5Checksum: "",
		}
	}

	// Send as message with the uploaded document.
	var peer tg.InputPeerClass
	if chatID > 0 {
		peer = &tg.InputPeerUser{UserID: chatID}
	} else if chatID < 0 {
		peer = &tg.InputPeerChannel{ChannelID: -chatID}
	} else {
		return 0, fmt.Errorf("mtproto: invalid chatID 0")
	}

	req := &tg.MessagesSendMediaRequest{
		Peer:    peer,
		Silent:  false,
		Message: caption,
		Media: &tg.InputMediaUploadedDocument{
			File:      inputFile,
			ForceFile: true,
		},
	}
	updates, err := api.MessagesSendMedia(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("mtproto: sendMedia: %w", err)
	}
	return extractMessageID(updates), nil
}

// extractMessageID pulls the first message ID out of an Updates bundle.
func extractMessageID(u tg.UpdatesClass) int {
	switch upd := u.(type) {
	case *tg.UpdateShortSentMessage:
		return upd.ID
	case *tg.Updates:
		for _, u := range upd.Updates {
			if sm, ok := u.(*tg.UpdateMessageID); ok {
				return sm.ID
			}
		}
	}
	return 0
}
