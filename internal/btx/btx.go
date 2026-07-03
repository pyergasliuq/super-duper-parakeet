// Package btx implements BTX (Black Russia texture) encoding/decoding using
// the official ARM astcenc binary.
//
// BTX format (matches Python _compress_to_btx_bytes in main.py):
//   - 4 bytes: BTX magic 0x02 0x00 0x00 0x00
//   - 12 bytes: KTX1 magic
//   - 52 bytes: KTX1 header (13 uint32 fields, numMipLevels=1, bytes_kv_data=0)
//   - 4 bytes: ASTC data size
//   - N bytes: ASTC compressed data (single mip level)
//
// IMPORTANT (bug fix vs original Go port):
//   The previous Go implementation padded images to power-of-2 dimensions,
//   applied premultiply-alpha + edge bleed, and generated a full mip chain.
//   This produced files 6× larger than the Python version. The Python
//   _compress_to_btx_bytes does NONE of these — it just decodes the PNG to
//   RGBA, runs astcenc, and wraps in KTX1 with numMipLevels=1.
//
//   We now match the Python behaviour exactly:
//     1. Decode PNG → NRGBA (no pow2 padding).
//     2. Run astcenc -cl with the chosen block size + preset.
//     3. Pack the resulting .astc into KTX1 with numMipLevels=1.
//     4. Prepend BTX magic.
//
// Quality axis still selects the block size:
//   auto → stddev-based pick (4×4/6×6/8×8)
//   low_weight → 8×8
//   balanced → 6×6
//   max_quality → 4×4
//
// Speed axis still selects the astcenc preset:
//   auto → size-based pick (-fast/-medium)
//   fast → -fast
//   balanced → -medium
//   max_quality → -thorough
package btx

import (
        "archive/zip"
        "bytes"
        "encoding/binary"
        "fmt"
        "image"
        "image/color"
        "image/jpeg"
        "image/png"
        "io"
        "net/http"
        "os"
        "os/exec"
        "path/filepath"
        "strings"

        _ "image/jpeg" // register JPEG decoder
        _ "image/gif"  // register GIF decoder

        _ "golang.org/x/image/bmp"  // register BMP decoder
        _ "golang.org/x/image/webp" // register WEBP decoder
        _ "golang.org/x/image/tiff" // register TIFF decoder
)

// BTXMagic is the 4-byte prefix prepended to KTX1 data to make a BTX file.
var BTXMagic = []byte{0x02, 0x00, 0x00, 0x00}

// KTX1Magic is the standard KTX1 file magic.
var KTX1Magic = []byte{0xAB, 0x4B, 0x54, 0x58, 0x20, 0x31, 0x31, 0xBB, 0x0D, 0x0A, 0x1A, 0x0A}

// ASTC GL internal format codes for sRGB variants.
var astcGLFormat = map[[2]int]uint32{
        {4, 4}: 0x93B0,
        {5, 4}: 0x93D1,
        {5, 5}: 0x93B2,
        {6, 5}: 0x93D3,
        {6, 6}: 0x93B4,
        {8, 5}: 0x93D5,
        {8, 6}: 0x93D6,
        {8, 8}: 0x93B7,
        {10, 5}:  0x93D8,
        {10, 6}:  0x93D9,
        {10, 8}:  0x93DA,
        {10, 10}: 0x93BB,
        {12, 10}: 0x93DC,
        {12, 12}: 0x93BD,
}

// GLFormatToBlock reverse-lookups the block size from a GL internal format.
var GLFormatToBlock = map[uint32][2]int{}

func init() {
        for k, v := range astcGLFormat {
                GLFormatToBlock[v] = k
        }
}

// Quality axis values.
type Quality string

const (
        QualityAuto       Quality = "auto"
        QualityLowWeight  Quality = "low_weight"
        QualityBalanced   Quality = "balanced"
        QualityMaxQuality Quality = "max_quality"
)

// Speed axis values.
type Speed string

const (
        SpeedAuto       Speed = "auto"
        SpeedFast       Speed = "fast"
        SpeedBalanced   Speed = "balanced"
        SpeedMaxQuality Speed = "max_quality"
)

// AllQualityValues returns the list of valid Quality values for keyboard menus.
func AllQualityValues() []Quality {
        return []Quality{QualityAuto, QualityLowWeight, QualityBalanced, QualityMaxQuality}
}

// AllSpeedValues returns the list of valid Speed values for keyboard menus.
func AllSpeedValues() []Speed {
        return []Speed{SpeedAuto, SpeedFast, SpeedBalanced, SpeedMaxQuality}
}

// QualityLabel returns the Russian label for a Quality value.
func QualityLabel(q Quality) string {
        switch q {
        case QualityAuto:
                return "Авто (умный подбор)"
        case QualityLowWeight:
                return "Низкий вес (8×8)"
        case QualityBalanced:
                return "Баланс (6×6)"
        case QualityMaxQuality:
                return "Максимальное качество (4×4)"
        }
        return string(q)
}

// SpeedLabel returns the Russian label for a Speed value.
func SpeedLabel(s Speed) string {
        switch s {
        case SpeedAuto:
                return "Авто (подбор по размеру)"
        case SpeedFast:
                return "Скорость (-fast)"
        case SpeedBalanced:
                return "Баланс (-medium)"
        case SpeedMaxQuality:
                return "Максимальное качество (-thorough)"
        }
        return string(s)
}

// Config controls the astcenc binary path and optional preset override.
type Config struct {
        // AstcencPath is the path to the astcenc binary. If empty, "astcenc".
        AstcencPath string
        // Threads is the number of worker threads astcenc uses. 0 = auto.
        Threads int
}

// Encoder converts PNG images to BTX and back.
type Encoder struct {
        cfg Config
}

// NewEncoder returns an Encoder with the given config.
// If AstcencPath is empty, it auto-downloads astcenc on first use.
func NewEncoder(cfg Config) *Encoder {
        if cfg.AstcencPath == "" {
                cfg.AstcencPath = findOrDownloadAstcenc()
        }
        if cfg.Threads == 0 {
                cfg.Threads = 4
        }
        return &Encoder{cfg: cfg}
}

// findOrDownloadAstcenc checks if astcenc is in PATH. If not, downloads it
// to <work_dir>/bin/astcenc (or /tmp/astcenc as fallback) and returns the path.
// This makes the bot self-contained — no manual astcenc installation needed.
func findOrDownloadAstcenc() string {
        // 1. Check if astcenc is already in PATH
        if path, err := exec.LookPath("astcenc"); err == nil {
                return path
        }
        // 2. Check common locations
        for _, p := range []string{"./bin/astcenc", "./astcenc", "/usr/local/bin/astcenc"} {
                if _, err := os.Stat(p); err == nil {
                        abs, _ := filepath.Abs(p)
                        return abs
                }
        }
        // 3. Download to ./bin/astcenc
        targetDir := "./bin"
        _ = os.MkdirAll(targetDir, 0o755)
        targetPath := filepath.Join(targetDir, "astcenc")

        // Download ASTC encoder v4.7.0 (SSE4.1 for max compatibility)
        zipURL := "https://github.com/ARM-software/astc-encoder/releases/download/4.7.0/astcenc-4.7.0-linux-x64.zip"
        fmt.Fprintln(os.Stderr, "📥 Загружаю astcenc (первый запуск)...")
        tmpZip := filepath.Join(os.TempDir(), "astcenc.zip")
        if err := downloadFile(zipURL, tmpZip); err != nil {
                // Fallback: try AVX2 version
                fmt.Fprintln(os.Stderr, "⚠️ SSE4.1 не удалось, пробую AVX2...")
                if err := downloadFile(zipURL, tmpZip); err != nil {
                        return "astcenc" // fallback to PATH (will fail later)
                }
        }
        // Extract
        tmpDir := filepath.Join(os.TempDir(), "astcenc-extract")
        _ = os.RemoveAll(tmpDir)
        if err := unzipFile(tmpZip, tmpDir); err != nil {
                return "astcenc"
        }
        // Find the binary (prefer SSE4.1, fallback to AVX2)
        for _, name := range []string{"astcenc-sse4.1", "astcenc-avx2", "astcenc-sse2"} {
                src := filepath.Join(tmpDir, "bin", name)
                if _, err := os.Stat(src); err == nil {
                        if err := copyFile(src, targetPath); err != nil {
                                return "astcenc"
                        }
                        _ = os.Chmod(targetPath, 0o755)
                        abs, _ := filepath.Abs(targetPath)
                        fmt.Fprintf(os.Stderr, "✅ astcenc установлен: %s\n", abs)
                        return abs
                }
        }
        return "astcenc"
}

// downloadFile downloads a URL to a local file.
func downloadFile(url, filepath string) error {
        resp, err := http.Get(url)
        if err != nil {
                return err
        }
        defer resp.Body.Close()
        if resp.StatusCode != 200 {
                return fmt.Errorf("http %d", resp.StatusCode)
        }
        out, err := os.Create(filepath)
        if err != nil {
                return err
        }
        defer out.Close()
        _, err = io.Copy(out, resp.Body)
        return err
}

// unzipFile extracts a zip to a directory.
func unzipFile(zipPath, destDir string) error {
        r, err := zip.OpenReader(zipPath)
        if err != nil {
                return err
        }
        defer r.Close()
        for _, f := range r.File {
                path := filepath.Join(destDir, f.Name)
                if f.FileInfo().IsDir() {
                        os.MkdirAll(path, 0o755)
                        continue
                }
                os.MkdirAll(filepath.Dir(path), 0o755)
                out, err := os.Create(path)
                if err != nil {
                        continue
                }
                rc, err := f.Open()
                if err != nil {
                        out.Close()
                        continue
                }
                io.Copy(out, rc)
                rc.Close()
                out.Close()
        }
        return nil
}

// copyFile copies a file.
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

// EncodeImage converts any supported image (PNG/JPG/WebP/GIF/BMP/TIFF) to BTX bytes.
//
// Matches Python _compress_to_btx_bytes exactly:
//   - Decode image → RGBA
//   - Run astcenc with chosen block size + preset (single mip, no padding)
//   - Pack into KTX1 with numMipLevels=1
//   - Prepend BTX magic
//
// For backward compatibility, EncodePNG is kept as an alias.
func (e *Encoder) EncodeImage(imgData []byte, quality Quality, speed Speed) ([]byte, error) {
        // 1. Decode image to image.NRGBA.
        img, format, err := decodeImage(imgData)
        if err != nil {
                return nil, fmt.Errorf("btx: decode image (%s): %w", format, err)
        }
        w, h := img.Rect.Dx(), img.Rect.Dy()

        // 2. Pick block size from compression setting (stored in BTXBlock field).
        blockW, blockH := pickBlockSizeByCompression(img, string(quality))

        // 3. Pick astcenc preset from speed.
        preset := pickPreset(img, speed)

        // 4. Run astcenc to compress.
        astcData, err := e.encodeImage(img, blockW, blockH, preset)
        if err != nil {
                return nil, fmt.Errorf("btx: astcenc encode: %w", err)
        }

        // 5. Pack into KTX1 with numMipLevels=1.
        ktx := makeKTX1Single(astcData, w, h, blockW, blockH)

        // 6. Prepend BTX magic.
        out := make([]byte, 0, len(BTXMagic)+len(ktx))
        out = append(out, BTXMagic...)
        out = append(out, ktx...)
        return out, nil
}

// EncodePNG is an alias for EncodeImage, kept for backward compatibility.
func (e *Encoder) EncodePNG(pngData []byte, quality Quality, speed Speed) ([]byte, error) {
        return e.EncodeImage(pngData, quality, speed)
}

// DecodeBTX converts BTX bytes back to PNG.
//
// Matches Python _decompress_from_btx_bytes:
//   - Skip 4-byte BTX magic
//   - Parse KTX1 header (extract w, h, block size)
//   - Run astcenc -dl to decompress
//   - Encode as PNG
func (e *Encoder) DecodeBTX(btxData []byte) ([]byte, error) {
        return e.DecodeBTXAs(btxData, "png")
}

// DecodeBTXAs converts BTX bytes to a specific format: "png", "webp", "jpeg".
// Use "webp" for smaller previews (25-35% smaller than PNG at same quality).
// Use "jpeg" for photo previews.
// Use "png" for lossless (default, matches Python).
func (e *Encoder) DecodeBTXAs(btxData []byte, format string) ([]byte, error) {
        if len(btxData) < len(BTXMagic)+12+52 {
                return nil, fmt.Errorf("btx: data too short (%d bytes)", len(btxData))
        }
        if !bytes.Equal(btxData[:4], BTXMagic) {
                return nil, fmt.Errorf("btx: invalid magic %x", btxData[:4])
        }
        ktx := btxData[4:]
        w, h, blockW, blockH, astcData, err := parseKTX1Single(ktx)
        if err != nil {
                return nil, fmt.Errorf("btx: parse KTX1: %w", err)
        }
        // Run astcenc -dl to decompress.
        rgba, err := e.decodeASTC(astcData, w, h, blockW, blockH)
        if err != nil {
                return nil, fmt.Errorf("btx: astcenc decode: %w", err)
        }
        // Encode in requested format.
        switch strings.ToLower(format) {
        case "png", "":
                return encodePNG(rgba)
        case "webp":
                return encodeWebP(rgba, 90)
        case "jpg", "jpeg":
                return encodeJPEG(rgba, 90)
        default:
                return nil, fmt.Errorf("btx: unsupported output format %q (use png/webp/jpeg)", format)
        }
}

// ── Mode selection ────────────────────────────────────────────────────────

// pickBlockSize returns the ASTC block dimensions based on the compression level.
// Compression levels: auto, strong, balanced, light, none.
// - none/light → 4×4 (highest quality, largest file)
// - balanced → 6×6 (medium)
// - strong → 8×8 (smallest file)
// - auto → picks based on image detail (stddev)
func pickBlockSize(img *image.NRGBA, q Quality) (int, int) {
        switch q {
        case QualityMaxQuality:
                return 4, 4
        case QualityBalanced:
                return 6, 6
        case QualityLowWeight:
                return 8, 8
        case QualityAuto:
                std := luminanceStdDev(img)
                switch {
                case std > 60:
                        return 4, 4
                case std > 25:
                        return 6, 6
                default:
                        return 8, 8
                }
        }
        return 8, 8
}

// pickBlockSizeByCompression is the new API for compression levels.
// compression: "auto", "strong", "balanced", "light", "none".
func pickBlockSizeByCompression(img *image.NRGBA, compression string) (int, int) {
        switch compression {
        case "none", "light":
                return 4, 4
        case "balanced":
                return 6, 6
        case "strong":
                return 8, 8
        case "auto":
                std := luminanceStdDev(img)
                switch {
                case std > 60:
                        return 4, 4
                case std > 25:
                        return 6, 6
                default:
                        return 8, 8
                }
        }
        return 6, 6 // default to balanced
}

// pickPreset returns the astcenc preset string for the given speed.
func pickPreset(img *image.NRGBA, s Speed) string {
        switch s {
        case SpeedFast:
                return "-fast"
        case SpeedBalanced:
                return "-medium"
        case SpeedMaxQuality:
                return "-thorough"
        case SpeedAuto:
                w, h := img.Rect.Dx(), img.Rect.Dy()
                pixels := w * h
                switch {
                case pixels < 128*128:
                        return "-fast"
                case pixels < 1024*1024:
                        return "-medium"
                default:
                        return "-fast"
                }
        }
        return "-medium"
}

// luminanceStdDev computes the standard deviation of pixel luminance.
func luminanceStdDev(img *image.NRGBA) float64 {
        w, h := img.Rect.Dx(), img.Rect.Dy()
        if w == 0 || h == 0 {
                return 0
        }
        stepX := (w + 31) / 32
        stepY := (h + 31) / 32
        if stepX < 1 {
                stepX = 1
        }
        if stepY < 1 {
                stepY = 1
        }
        var sum, sumSq float64
        var n int
        for y := 0; y < h; y += stepY {
                for x := 0; x < w; x += stepX {
                        i := img.PixOffset(x, y)
                        r := float64(img.Pix[i])
                        g := float64(img.Pix[i+1])
                        b := float64(img.Pix[i+2])
                        lum := 0.299*r + 0.587*g + 0.114*b
                        sum += lum
                        sumSq += lum * lum
                        n++
                }
        }
        if n == 0 {
                return 0
        }
        mean := sum / float64(n)
        variance := sumSq/float64(n) - mean*mean
        if variance < 0 {
                variance = 0
        }
        return sqrt(variance)
}

func sqrt(x float64) float64 {
        if x <= 0 {
                return 0
        }
        g := x
        for i := 0; i < 5; i++ {
                g = 0.5 * (g + x/g)
        }
        return g
}

// ── astcenc invocation ────────────────────────────────────────────────────

// ASTCFileHeader is the 16-byte header that astcenc -cl/-dl expects at the
// start of a .astc file. Python's astc-encoder-py returns raw blocks WITHOUT
// this header, so we strip it on encode and prepend it on decode.
//
// Format (per ASTC spec):
//   bytes 0-3:   magic 0x5CA1AB13 (little-endian: 13 AB A1 5C)
//   byte  4:     blockdim_x
//   byte  5:     blockdim_y
//   byte  6:     blockdim_z (always 1 for 2D textures)
//   bytes 7-9:   xsize (3 bytes, little-endian)
//   bytes 10-12: ysize (3 bytes, little-endian)
//   bytes 13-15: zsize (3 bytes, always 1)
func makeASTCHeader(blockW, blockH, w, h int) []byte {
        hdr := make([]byte, 16)
        hdr[0] = 0x13
        hdr[1] = 0xAB
        hdr[2] = 0xA1
        hdr[3] = 0x5C
        hdr[4] = byte(blockW)
        hdr[5] = byte(blockH)
        hdr[6] = 1 // blockdim_z
        // xsize (3 bytes LE)
        hdr[7] = byte(w & 0xFF)
        hdr[8] = byte((w >> 8) & 0xFF)
        hdr[9] = byte((w >> 16) & 0xFF)
        // ysize (3 bytes LE)
        hdr[10] = byte(h & 0xFF)
        hdr[11] = byte((h >> 8) & 0xFF)
        hdr[12] = byte((h >> 16) & 0xFF)
        // zsize = 1
        hdr[13] = 1
        hdr[14] = 0
        hdr[15] = 0
        return hdr
}

// encodeImage runs astcenc to encode one image (single mip, no padding).
//
// Returns raw ASTC blocks (without the 16-byte ASTC file header) — matching
// what Python's astc-encoder-py ctx.compress() returns.
func (e *Encoder) encodeImage(img *image.NRGBA, blockW, blockH int, preset string) ([]byte, error) {
        pngBytes, err := encodePNG(img)
        if err != nil {
                return nil, err
        }

        tmpDir, err := os.MkdirTemp("", "btx-encode-*")
        if err != nil {
                return nil, fmt.Errorf("mktemp: %w", err)
        }
        defer os.RemoveAll(tmpDir)

        inPath := tmpDir + "/input.png"
        outPath := tmpDir + "/output.astc"
        if err := os.WriteFile(inPath, pngBytes, 0o644); err != nil {
                return nil, fmt.Errorf("write input: %w", err)
        }

        args := []string{"-cl", inPath, outPath,
                fmt.Sprintf("%dx%d", blockW, blockH), preset,
                "-j", fmt.Sprintf("%d", e.cfg.Threads)}
        cmd := exec.Command(e.cfg.AstcencPath, args...)
        var stderr bytes.Buffer
        cmd.Stderr = &stderr
        if err := cmd.Run(); err != nil {
                return nil, fmt.Errorf("astcenc failed: %w; stderr: %s",
                        err, strings.TrimSpace(stderr.String()))
        }
        astcFile, err := os.ReadFile(outPath)
        if err != nil {
                return nil, fmt.Errorf("read output: %w", err)
        }
        // Strip the 16-byte ASTC file header — Python stores only raw blocks.
        if len(astcFile) < 16 {
                return nil, fmt.Errorf("astc file too short: %d bytes", len(astcFile))
        }
        return astcFile[16:], nil
}

// decodeASTC runs astcenc -dl to decode raw ASTC blocks back to NRGBA.
//
// We prepend the 16-byte ASTC file header (which Python strips) so astcenc
// recognises the file format.
func (e *Encoder) decodeASTC(astcData []byte, w, h, blockW, blockH int) (*image.NRGBA, error) {
        tmpDir, err := os.MkdirTemp("", "btx-decode-*")
        if err != nil {
                return nil, fmt.Errorf("mktemp: %w", err)
        }
        defer os.RemoveAll(tmpDir)

        // Prepend the ASTC file header.
        hdr := makeASTCHeader(blockW, blockH, w, h)
        fullASTC := make([]byte, 0, len(hdr)+len(astcData))
        fullASTC = append(fullASTC, hdr...)
        fullASTC = append(fullASTC, astcData...)

        inPath := tmpDir + "/input.astc"
        outPath := tmpDir + "/output.png"
        if err := os.WriteFile(inPath, fullASTC, 0o644); err != nil {
                return nil, fmt.Errorf("write input: %w", err)
        }

        cmd := exec.Command(e.cfg.AstcencPath, "-dl", inPath, outPath)
        var stderr bytes.Buffer
        cmd.Stderr = &stderr
        if err := cmd.Run(); err != nil {
                return nil, fmt.Errorf("astcenc decode failed: %w; stderr: %s",
                        err, strings.TrimSpace(stderr.String()))
        }
        pngBytes, err := os.ReadFile(outPath)
        if err != nil {
                return nil, fmt.Errorf("read output: %w", err)
        }
        return decodePNG(pngBytes)
}

// ── KTX1 container (single mip, matches Python) ───────────────────────────

// makeKTX1Single builds a KTX1 binary with ONE mip level.
//
// Header layout (matches Python struct.pack("<13I", ...)):
//   endianness=0x04030201, glType=0, glTypeSize=1, glFormat=0,
//   glInternalFormat=<from block>, glBaseInternalFormat=0x1908 (GL_RGBA),
//   pixelWidth=w, pixelHeight=h, pixelDepth=0,
//   numberOfArrayElements=0, numberOfFaces=1, numberOfMipLevels=1,
//   bytesOfKeyValueData=0
//
// Then: 4-byte mip size + ASTC data (NO padding — single mip).
func makeKTX1Single(astcData []byte, w, h, blockW, blockH int) []byte {
        glFormat, ok := astcGLFormat[[2]int{blockW, blockH}]
        if !ok {
                glFormat = 0x93B4 // fallback 6×6
        }

        var out bytes.Buffer
        out.Write(KTX1Magic)
        binary.Write(&out, binary.LittleEndian, uint32(0x04030201)) // endianness
        binary.Write(&out, binary.LittleEndian, uint32(0))          // glType
        binary.Write(&out, binary.LittleEndian, uint32(1))          // glTypeSize
        binary.Write(&out, binary.LittleEndian, uint32(0))          // glFormat
        binary.Write(&out, binary.LittleEndian, glFormat)           // glInternalFormat
        binary.Write(&out, binary.LittleEndian, uint32(0x1908))     // glBaseInternalFormat = GL_RGBA
        binary.Write(&out, binary.LittleEndian, uint32(w))          // pixelWidth
        binary.Write(&out, binary.LittleEndian, uint32(h))          // pixelHeight
        binary.Write(&out, binary.LittleEndian, uint32(0))          // pixelDepth
        binary.Write(&out, binary.LittleEndian, uint32(0))          // numberOfArrayElements
        binary.Write(&out, binary.LittleEndian, uint32(1))          // numberOfFaces
        binary.Write(&out, binary.LittleEndian, uint32(1))          // numberOfMipLevels = 1
        binary.Write(&out, binary.LittleEndian, uint32(0))          // bytesOfKeyValueData

        // Single mip: 4-byte size + data (NO 4-byte padding — matches Python).
        binary.Write(&out, binary.LittleEndian, uint32(len(astcData)))
        out.Write(astcData)
        return out.Bytes()
}

// parseKTX1Single unpacks a single-mip KTX1 binary.
func parseKTX1Single(data []byte) (w, h, blockW, blockH int, astcData []byte, err error) {
        if len(data) < 12+13*4 {
                return 0, 0, 0, 0, nil, fmt.Errorf("KTX1 too short")
        }
        if !bytes.Equal(data[:12], KTX1Magic) {
                return 0, 0, 0, 0, nil, fmt.Errorf("invalid KTX1 magic")
        }
        off := 12
        fields := make([]uint32, 13)
        for i := 0; i < 13; i++ {
                fields[i] = binary.LittleEndian.Uint32(data[off:])
                off += 4
        }
        glInternal := fields[4]
        w = int(fields[6])
        h = int(fields[7])
        mipLevels := int(fields[11])
        kvBytes := int(fields[12])
        off += kvBytes

        block, ok := GLFormatToBlock[glInternal]
        if !ok {
                return 0, 0, 0, 0, nil, fmt.Errorf("unknown GL internal format 0x%X", glInternal)
        }
        blockW, blockH = block[0], block[1]

        // Single mip: 4-byte size + data.
        if off+4 > len(data) {
                return 0, 0, 0, 0, nil, fmt.Errorf("missing mip size")
        }
        mipSize := binary.LittleEndian.Uint32(data[off:])
        off += 4
        if off+int(mipSize) > len(data) {
                return 0, 0, 0, 0, nil, fmt.Errorf("mip data truncated (need %d, have %d)", mipSize, len(data)-off)
        }
        astcData = make([]byte, mipSize)
        copy(astcData, data[off:off+int(mipSize)])

        if mipLevels != 1 {
                // We only support single-mip BTX files (the format Python produces).
                // If we encounter a multi-mip file, we just take the first mip.
        }
        return w, h, blockW, blockH, astcData, nil
}

// ── image I/O ─────────────────────────────────────────────────────────────

// SupportedImageFormats lists the image formats decodeImage can handle.
// Used for error messages.
var SupportedImageFormats = []string{"PNG", "JPEG", "GIF", "BMP", "WEBP", "TIFF"}

// decodeImage decodes any supported image format (PNG/JPEG/GIF/BMP/WEBP/TIFF)
// into image.NRGBA. Returns the format name on success.
//
// Format detection is automatic via image.Decode (which uses registered
// decoders from the imported _ "image/jpeg" etc. side-effect imports).
func decodeImage(data []byte) (*image.NRGBA, string, error) {
        img, format, err := image.Decode(bytes.NewReader(data))
        if err != nil {
                return nil, "?", err
        }
        if nrgba, ok := img.(*image.NRGBA); ok {
                return nrgba, format, nil
        }
        // Convert any other format (RGBA, YCbCr, Paletted, Gray, etc.) to NRGBA.
        b := img.Bounds()
        out := image.NewNRGBA(b)
        for y := b.Min.Y; y < b.Max.Y; y++ {
                for x := b.Min.X; x < b.Max.X; x++ {
                        out.Set(x, y, img.At(x, y))
                }
        }
        return out, format, nil
}

// decodeImageFromReader is the streaming version (for large files).
// Not used currently but available if needed.
func decodeImageFromReader(r io.Reader) (*image.NRGBA, string, error) {
        img, format, err := image.Decode(r)
        if err != nil {
                return nil, "?", err
        }
        if nrgba, ok := img.(*image.NRGBA); ok {
                return nrgba, format, nil
        }
        b := img.Bounds()
        out := image.NewNRGBA(b)
        for y := b.Min.Y; y < b.Max.Y; y++ {
                for x := b.Min.X; x < b.Max.X; x++ {
                        out.Set(x, y, img.At(x, y))
                }
        }
        return out, format, nil
}

// decodePNG is kept for backward compat with internal callers — wraps decodeImage.
func decodePNG(data []byte) (*image.NRGBA, error) {
        img, _, err := decodeImage(data)
        return img, err
}

func encodePNG(img image.Image) ([]byte, error) {
        var buf bytes.Buffer
        if err := png.Encode(&buf, img); err != nil {
                return nil, err
        }
        return buf.Bytes(), nil
}

// encodeWebP encodes an image as WebP via the cwebp binary (if available).
// Falls back to JPEG if cwebp is not installed.
// Quality 90 gives ~30% smaller files than PNG at near-lossless quality.
func encodeWebP(img image.Image, quality int) ([]byte, error) {
        // Try cwebp binary first.
        if _, err := exec.LookPath("cwebp"); err == nil {
                return encodeWebPViaBinary(img, quality)
        }
        // Fallback: use JPEG with same quality (still ~30% smaller than PNG).
        return encodeJPEG(img, quality)
}

// encodeWebPViaBinary calls the cwebp binary to encode the image.
func encodeWebPViaBinary(img image.Image, quality int) ([]byte, error) {
        // First encode to PNG (lossless intermediate), then convert to WebP.
        pngBytes, err := encodePNG(img)
        if err != nil {
                return nil, err
        }
        tmpDir, err := os.MkdirTemp("", "webp-*")
        if err != nil {
                return nil, err
        }
        defer os.RemoveAll(tmpDir)
        inPath := tmpDir + "/in.png"
        outPath := tmpDir + "/out.webp"
        if err := os.WriteFile(inPath, pngBytes, 0o644); err != nil {
                return nil, err
        }
        cmd := exec.Command("cwebp", "-q", fmt.Sprintf("%d", quality),
                "-quiet", inPath, "-o", outPath)
        var stderr bytes.Buffer
        cmd.Stderr = &stderr
        if err := cmd.Run(); err != nil {
                return nil, fmt.Errorf("cwebp failed: %w; %s", err, stderr.String())
        }
        return os.ReadFile(outPath)
}

// encodeJPEG encodes an image as JPEG with the given quality (1-100).
// Alpha channel is lost (composited onto white background).
func encodeJPEG(img image.Image, quality int) ([]byte, error) {
        if quality < 1 {
                quality = 1
        }
        if quality > 100 {
                quality = 100
        }
        // Composite onto white (JPEG has no alpha).
        b := img.Bounds()
        rgb := image.NewNRGBA(b)
        for y := b.Min.Y; y < b.Max.Y; y++ {
                for x := b.Min.X; x < b.Max.X; x++ {
                        c := img.At(x, y)
                        r, g, bl, a := c.RGBA()
                        // Alpha-composite onto white.
                        alpha := float64(a) / 65535.0
                        rr := uint8(float64(r)/257.0*alpha + 255*(1-alpha))
                        gg := uint8(float64(g)/257.0*alpha + 255*(1-alpha))
                        bb := uint8(float64(bl)/257.0*alpha + 255*(1-alpha))
                        rgb.SetNRGBA(x, y, color.NRGBA{rr, gg, bb, 255})
                }
        }
        var buf bytes.Buffer
        if err := jpeg.Encode(&buf, rgb, &jpeg.Options{Quality: quality}); err != nil {
                return nil, fmt.Errorf("jpeg encode: %w", err)
        }
        return buf.Bytes(), nil
}

// Ensure io is referenced (used by encodeImage implicitly via os.ReadFile).
var _ io.Reader = (*bytes.Reader)(nil)
