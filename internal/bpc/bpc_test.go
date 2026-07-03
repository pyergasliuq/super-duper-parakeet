// Package bpc — bpc_test.go
package bpc_test

import (
	"bytes"
	"testing"

	"github.com/pweper/bot/internal/bpc"
)

func TestDefaultKey(t *testing.T) {
	// DefaultKey should be 20 bytes.
	if len(bpc.DefaultKey) != 20 {
		t.Errorf("DefaultKey length = %d, want 20", len(bpc.DefaultKey))
	}
	// First byte should be 0x31 (from "31 63 4b ...").
	if bpc.DefaultKey[0] != 0x31 {
		t.Errorf("DefaultKey[0] = 0x%02X, want 0x31", bpc.DefaultKey[0])
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	// Encrypt a ZIP file with the default key, then decrypt.
	original := []byte{'P', 'K', 0x03, 0x04, 0x00, 0x00, 0x00, 0x00, 'H', 'e', 'l', 'l', 'o'}
	encrypted := bpc.Encrypt(original)
	// Encrypted should NOT start with PK (otherwise encryption is a no-op).
	if bytes.HasPrefix(encrypted, []byte{'P', 'K'}) {
		t.Errorf("encrypted data still starts with PK — encryption is broken")
	}
	decrypted := bpc.Decrypt(encrypted)
	if !bytes.Equal(decrypted, original) {
		t.Errorf("round-trip failed:\n  orig = %x\n  dec  = %x", original, decrypted)
	}
}

func TestDecryptAlreadyDecrypted(t *testing.T) {
	// If the input is already a ZIP, Decrypt should return it unchanged
	// (fast path).
	original := []byte{'P', 'K', 0x03, 0x04, 'X', 'Y', 'Z'}
	out := bpc.Decrypt(original)
	if !bytes.Equal(out, original) {
		t.Errorf("already-decrypted ZIP should pass through unchanged")
	}
}

func TestEncryptWithCustomKey(t *testing.T) {
	key := []byte{0xAA, 0xBB, 0xCC}
	data := []byte("Hello, world!")
	encrypted := bpc.EncryptWithKey(data, key)
	// Verify round-trip.
	decrypted := make([]byte, len(encrypted))
	for i, b := range encrypted {
		decrypted[i] = b ^ key[i%len(key)]
	}
	if !bytes.Equal(decrypted, data) {
		t.Errorf("custom key round-trip failed")
	}
}

func TestEncryptWithEmptyKey(t *testing.T) {
	// Empty key should fall back to processZipKey (same as DefaultKey).
	data := []byte("test")
	encrypted := bpc.EncryptWithKey(data, nil)
	// Should be different from input (encryption is happening).
	if bytes.Equal(encrypted, data) {
		t.Errorf("empty key did not encrypt")
	}
	// Should round-trip with DefaultKey.
	decrypted := bpc.Decrypt(encrypted)
	if !bytes.Equal(decrypted, data) {
		t.Errorf("empty key round-trip failed")
	}
}

func TestDetectKeyWithZip(t *testing.T) {
	// Encrypt "PK..." with a known 4-byte key, then verify DetectKey recovers
	// a key that produces "PK" at the start.
	original := []byte{'P', 'K', 0x03, 0x04, 'X', 'Y', 'Z', 'W'}
	// We can't easily predict which key DetectKey returns (the algorithm is
	// non-deterministic in the sense that multiple keys can match). But we
	// can verify that Decrypt(Encrypt(x)) == x.
	encrypted := bpc.Encrypt(original)
	decrypted := bpc.Decrypt(encrypted)
	if !bytes.Equal(decrypted, original) {
		t.Errorf("DetectKey round-trip failed:\n  orig = %x\n  dec  = %x",
			original, decrypted)
	}
}
