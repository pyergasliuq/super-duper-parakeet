// Package ziputil provides safe ZIP-reading utilities with bomb protection.
//
// A "zip bomb" is a small ZIP archive that decompresses to a huge size
// (e.g., 42 KB → 4.5 PB). Without protection this can OOM the bot.
//
// SafeReader enforces:
//   - Max total uncompressed size (default 500 MB)
//   - Max files per archive (default 5000)
//   - Max file size for any single file (default 200 MB)
package ziputil

import (
	"archive/zip"
	"fmt"
	"io"
)

// Limits defines the safety limits for ZIP extraction.
type Limits struct {
	MaxTotalUncompressed int64 // default 500 MB
	MaxFiles             int   // default 5000
	MaxSingleFile        int64 // default 200 MB
}

// DefaultLimits returns sensible defaults.
func DefaultLimits() Limits {
	return Limits{
		MaxTotalUncompressed: 500 * 1024 * 1024,
		MaxFiles:             5000,
		MaxSingleFile:        200 * 1024 * 1024,
	}
}

// ErrBomb is returned when a ZIP exceeds the safety limits.
type ErrBomb struct {
	Reason string
}

func (e *ErrBomb) Error() string { return "zip bomb protection: " + e.Reason }

// SafeReader wraps a *zip.Reader with safety checks.
type SafeReader struct {
	*zip.Reader
	limits Limits
}

// NewSafeReader returns a SafeReader with the given limits.
// Pass nil for defaults.
func NewSafeReader(r *zip.Reader, limits *Limits) (*SafeReader, error) {
	if limits == nil {
		l := DefaultLimits()
		limits = &l
	}
	sr := &SafeReader{Reader: r, limits: *limits}
	if err := sr.validate(); err != nil {
		return nil, err
	}
	return sr, nil
}

// validate checks the ZIP's declared sizes against limits.
func (s *SafeReader) validate() error {
	if len(s.Reader.File) > s.limits.MaxFiles {
		return &ErrBomb{Reason: fmt.Sprintf("too many files: %d > %d", len(s.Reader.File), s.limits.MaxFiles)}
	}
	var total int64
	for _, f := range s.Reader.File {
		sz := int64(f.UncompressedSize64)
		if sz > s.limits.MaxSingleFile {
			return &ErrBomb{Reason: fmt.Sprintf("file %q too large: %d > %d", f.Name, sz, s.limits.MaxSingleFile)}
		}
		total += sz
		if total > s.limits.MaxTotalUncompressed {
			return &ErrBomb{Reason: fmt.Sprintf("total uncompressed %d > %d", total, s.limits.MaxTotalUncompressed)}
		}
	}
	return nil
}

// ReadFile safely extracts one file from the ZIP, returning its bytes.
// Enforces the single-file limit at decompression time too (in case the
// declared size was a lie).
func (s *SafeReader) ReadFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	// Cap at MaxSingleFile + 1 byte to detect overrun.
	cap := s.limits.MaxSingleFile + 1
	buf := make([]byte, 0, f.UncompressedSize64)
	tmp := make([]byte, 32*1024)
	for {
		n, err := rc.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if int64(len(buf)) > cap {
				return nil, &ErrBomb{Reason: fmt.Sprintf("file %q exceeded limit during decompression", f.Name)}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return buf, nil
}
