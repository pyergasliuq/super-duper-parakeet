// Package ifp converts GTA SA animation files: .ifp (Black Russia encrypted)
// → .ani (standard GTA SA animation).
//
// Original Python (main.py:4893-4911):
//   with open(download_path, 'rb') as f_input, open(ani_file_path, 'wb') as f_output:
//       f_input.seek(8)
//       byte = f_input.read(8)
//       while byte: f_output.write(byte); byte = f_input.read(8)
//   with open(ani_file_path, 'rb') as f: original_data = f.read()
//   new_data = b'\x41\x4E\x50\x33' + original_data  # "ANP3" prefix
//   with open(ani_file_path, 'wb') as er: er.write(new_data)
//
// What it does:
//   1. Skip the first 8 bytes of .ifp (BR encryption header).
//   2. Copy the rest to .ani (chunked in 8-byte blocks — purely Python
//      artefact, in Go we just copy the rest in one go).
//   3. Prepend the 4-byte magic "ANP3".
//
// Bug in Python: the chunked read(8) loop writes 8 bytes at a time but
// actually writes whatever `byte` contains (which is 8 bytes except possibly
// the last chunk). The last partial chunk is also written verbatim, so the
// output is identical to "skip 8 bytes + prepend ANP3". We replicate that.
package ifp

import (
	"bytes"
	"fmt"
	"io"
)

// Magic is the 4-byte prefix prepended to .ani files.
var Magic = []byte{'A', 'N', 'P', '3'}

// HeaderSize is the number of bytes to skip from the start of .ifp.
const HeaderSize = 8

// Convert reads an .ifp file and returns the corresponding .ani bytes.
// Returns an error if input is shorter than HeaderSize.
func Convert(ifpData []byte) ([]byte, error) {
	if len(ifpData) < HeaderSize {
		return nil, fmt.Errorf("ifp: input too short (%d bytes, need at least %d)",
			len(ifpData), HeaderSize)
	}
	body := ifpData[HeaderSize:]
	out := make([]byte, 0, len(Magic)+len(body))
	out = append(out, Magic...)
	out = append(out, body...)
	return out, nil
}

// ConvertReader is the streaming version: reads from r, writes to w.
// Useful for large files where we don't want to buffer the whole thing.
func ConvertReader(r io.Reader, w io.Writer) error {
	// Skip the 8-byte header.
	if _, err := io.CopyN(io.Discard, r, HeaderSize); err != nil {
		return fmt.Errorf("ifp: skip header: %w", err)
	}
	// Write the magic.
	if _, err := w.Write(Magic); err != nil {
		return fmt.Errorf("ifp: write magic: %w", err)
	}
	// Stream the rest.
	if _, err := io.Copy(w, r); err != nil {
		return fmt.Errorf("ifp: copy body: %w", err)
	}
	return nil
}

// IsIFP returns true if the data looks like a .ifp file (just checks length
// for now — BR .ifp files have no magic of their own).
func IsIFP(data []byte) bool {
	return len(data) > HeaderSize
}

// Ensure bytes.Buffer is referenced (used internally by some callers).
var _ = bytes.NewBuffer
