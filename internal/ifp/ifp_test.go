// Package ifp — ifp_test.go
package ifp_test

import (
	"bytes"
	"testing"

	"github.com/pweper/bot/internal/ifp"
)

func TestConvert(t *testing.T) {
	// Build a fake .ifp: 8-byte header + "BODY" + zeroes.
	header := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	body := []byte("BODY1234")
	ifpData := append(header, body...)

	out, err := ifp.Convert(ifpData)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	// Expected: ANP3 + body.
	want := append([]byte("ANP3"), body...)
	if !bytes.Equal(out, want) {
		t.Errorf("output = %x, want %x", out, want)
	}
}

func TestConvertTooShort(t *testing.T) {
	_, err := ifp.Convert([]byte{1, 2, 3})
	if err == nil {
		t.Errorf("expected error for short input, got nil")
	}
}

func TestConvertReader(t *testing.T) {
	header := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	body := []byte("STREAMED_BODY_DATA")
	ifpData := append(header, body...)

	r := bytes.NewReader(ifpData)
	var w bytes.Buffer
	if err := ifp.ConvertReader(r, &w); err != nil {
		t.Fatalf("ConvertReader: %v", err)
	}
	want := append([]byte("ANP3"), body...)
	if !bytes.Equal(w.Bytes(), want) {
		t.Errorf("output = %x, want %x", w.Bytes(), want)
	}
}

func TestIsIFP(t *testing.T) {
	if ifp.IsIFP([]byte{1, 2}) {
		t.Errorf("IsIFP should be false for 2-byte input")
	}
	if !ifp.IsIFP([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9}) {
		t.Errorf("IsIFP should be true for 9-byte input")
	}
}
