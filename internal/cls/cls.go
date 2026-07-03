// Package cls converts GTA SA collision files: .cls (Black Russia encrypted)
// → .col (standard GTA SA collision).
//
// Original Python (main.py:4928-4946):
//   with open(download_path, 'rb') as f_input, open(ani_file_path, 'wb') as f_output:
//       f_input.seek(4)
//       byte = f_input.read(4)
//       while byte: f_output.write(byte); byte = f_input.read(4)
//   new_data = b'\x43\x4F\x4C\x33' + original_data  # "COL3" prefix
//
// Same pattern as .ifp but with 4-byte header skip and "COL3" magic.
package cls

import (
	"fmt"
	"io"
)

// Magic is the 4-byte prefix prepended to .col files.
var Magic = []byte{'C', 'O', 'L', '3'}

// HeaderSize is the number of bytes to skip from the start of .cls.
const HeaderSize = 4

// Convert reads a .cls file and returns the corresponding .col bytes.
func Convert(clsData []byte) ([]byte, error) {
	if len(clsData) < HeaderSize {
		return nil, fmt.Errorf("cls: input too short (%d bytes, need at least %d)",
			len(clsData), HeaderSize)
	}
	body := clsData[HeaderSize:]
	out := make([]byte, 0, len(Magic)+len(body))
	out = append(out, Magic...)
	out = append(out, body...)
	return out, nil
}

// ConvertReader is the streaming version.
func ConvertReader(r io.Reader, w io.Writer) error {
	if _, err := io.CopyN(io.Discard, r, HeaderSize); err != nil {
		return fmt.Errorf("cls: skip header: %w", err)
	}
	if _, err := w.Write(Magic); err != nil {
		return fmt.Errorf("cls: write magic: %w", err)
	}
	if _, err := io.Copy(w, r); err != nil {
		return fmt.Errorf("cls: copy body: %w", err)
	}
	return nil
}
