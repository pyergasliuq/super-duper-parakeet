// Package cls — cls_test.go
package cls_test

import (
	"bytes"
	"testing"

	"github.com/pweper/bot/internal/cls"
)

func TestConvert(t *testing.T) {
	header := []byte{0x00, 0x01, 0x02, 0x03}
	body := []byte("COLLISION_DATA_HERE")
	clsData := append(header, body...)

	out, err := cls.Convert(clsData)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	want := append([]byte("COL3"), body...)
	if !bytes.Equal(out, want) {
		t.Errorf("output = %x, want %x", out, want)
	}
}

func TestConvertTooShort(t *testing.T) {
	_, err := cls.Convert([]byte{1, 2})
	if err == nil {
		t.Errorf("expected error for 2-byte input, got nil")
	}
}

func TestConvertReader(t *testing.T) {
	header := []byte{0xAA, 0xBB, 0xCC, 0xDD}
	body := []byte("XYZ")
	clsData := append(header, body...)

	r := bytes.NewReader(clsData)
	var w bytes.Buffer
	if err := cls.ConvertReader(r, &w); err != nil {
		t.Fatalf("ConvertReader: %v", err)
	}
	want := append([]byte("COL3"), body...)
	if !bytes.Equal(w.Bytes(), want) {
		t.Errorf("output = %x, want %x", w.Bytes(), want)
	}
}
