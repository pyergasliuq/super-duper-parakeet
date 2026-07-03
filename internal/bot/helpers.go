// Package bot — helpers.go — small utilities used by handlers.
package bot

import (
        "archive/zip"
        "context"
        "io"
        "net/http"
        "strings"
)

// ── ZIP helpers ────────────────────────────────────────────────────────────

// zipReader wraps a *zip.Reader for reading.
type zipReader struct {
        *zip.Reader
}

func openZipReader(data []byte) (*zipReader, error) {
        r, err := zip.NewReader(bytesReader(data), int64(len(data)))
        if err != nil {
                return nil, err
        }
        return &zipReader{Reader: r}, nil
}

// zipWriter wraps a *zip.Writer for writing to a buffer.
type zipWriter struct {
        w *zip.Writer
}

func newZipWriter(buf io.Writer) *zipWriter {
        return &zipWriter{w: zip.NewWriter(buf)}
}

// writeFile adds a file to the zip.
func (zw *zipWriter) writeFile(name string, data []byte) {
        f, err := zw.w.Create(name)
        if err != nil {
                return
        }
        _, _ = f.Write(data)
}

// copyFile copies an existing zip.File into this zip without decompressing.
func (zw *zipWriter) copyFile(f *zip.File) {
        out, err := zw.w.Create(f.Name)
        if err != nil {
                return
        }
        rc, err := f.Open()
        if err != nil {
                return
        }
        defer rc.Close()
        _, _ = io.Copy(out, rc)
}

func (zw *zipWriter) close() {
        _ = zw.w.Close()
}

// bytesReader is a tiny helper that returns an io.ReaderAt for a []byte.
// We avoid importing bytes here to keep dependencies minimal in this file.
func bytesReader(b []byte) io.ReaderAt {
        return &byteSliceReader{b: b}
}

type byteSliceReader struct{ b []byte }

func (r *byteSliceReader) ReadAt(p []byte, off int64) (int, error) {
        if off >= int64(len(r.b)) {
                return 0, io.EOF
        }
        n := copy(p, r.b[off:])
        if off+int64(n) < int64(len(r.b)) {
                return n, nil
        }
        return n, io.EOF
}

// ── HTTP helpers ───────────────────────────────────────────────────────────

var httpClient = &http.Client{}

func newHTTPRequest(ctx context.Context, url string) (*http.Request, error) {
        req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
        if err != nil {
                return nil, err
        }
        return req, nil
}

// ── image extension helpers ───────────────────────────────────────────────

// imageExts is the set of image file extensions we accept for processing.
var imageExts = map[string]bool{
        ".png":  true,
        ".jpg":  true,
        ".jpeg": true,
        ".webp": true,
        ".gif":  true,
        ".bmp":  true,
        ".tif":  true,
        ".tiff": true,
}

// isImageFile returns true if the filename has an image extension we support.
func isImageFile(name string) bool {
        lower := strings.ToLower(name)
        for ext := range imageExts {
                if strings.HasSuffix(lower, ext) {
                        return true
                }
        }
        return false
}
