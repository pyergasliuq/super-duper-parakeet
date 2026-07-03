// Package imaging — filters_extra.go — additional filters for /filters command.
//
// All filters are accessible via /filters <name> [amount]:
//   blur, sharpen, vignette, grain, threshold, dither, rotate, crop,
//   watermark, collage
package imaging

import (
        "fmt"
        "image"
        "image/color"
        "math"
        "strings"
)

// FilterExtra applies the extended filter set. Returns (output, handled).
// If the filter name is not recognized, returns (nil, false) so the caller
// can fall back to the basic pixel-loop filters.
func FilterExtra(img *image.NRGBA, name string, amount int, args []string) ([]byte, bool, error) {
        name = strings.ToLower(name)
        if amount < 0 {
                amount = 0
        }
        if amount > 100 {
                amount = 100
        }
        if amount == 0 {
                amount = 50
        }

        switch name {
        case "blur":
                out, err := applyBlur(img, amount)
                return out, true, err
        case "sharpen":
                out, err := applySharpen(img, amount)
                return out, true, err
        case "vignette":
                out, err := applyVignette(img, amount)
                return out, true, err
        case "grain":
                out, err := applyGrain(img, amount)
                return out, true, err
        case "threshold":
                out, err := applyThreshold(img, amount)
                return out, true, err
        case "dither":
                out, err := applyDither(img, amount)
                return out, true, err
        case "rotate":
                angle := float64(amount) * 3.6 // 0-100 → 0-360 degrees
                if len(args) > 0 {
                        if a, err := parseFloat(args[0]); err == nil {
                                angle = a
                        }
                }
                out, err := applyRotate(img, angle)
                return out, true, err
        case "crop":
                out, err := applyCrop(img, args)
                return out, true, err
        case "watermark":
                text := ""
                if len(args) > 0 {
                        text = strings.Join(args, " ")
                }
                out, err := applyWatermark(img, text, amount)
                return out, true, err
        case "collage":
                out, err := applyCollage(img, amount)
                return out, true, err
        }
        return nil, false, nil
}

// applyBlur — Gaussian-like blur using box blur (3 passes ≈ Gaussian).
// amount 0-100 → radius 1-20.
func applyBlur(img *image.NRGBA, amount int) ([]byte, error) {
        radius := 1 + amount/5 // 1..21
        if radius > 20 {
                radius = 20
        }
        // 3 passes of box blur ≈ Gaussian.
        out := boxBlur(img, radius)
        out = boxBlur(out, radius)
        out = boxBlur(out, radius)
        return EncodePNG(out)
}

// applySharpen — unsharp mask: out = img + (img - blurred) * strength.
// amount 0-100 → strength 0.5..5.0.
func applySharpen(img *image.NRGBA, amount int) ([]byte, error) {
        strength := 0.5 + float64(amount)/25.0 // 0.5..4.5
        blurred := boxBlur(img, 1)
        out := image.NewNRGBA(img.Rect)
        for y := 0; y < img.Rect.Dy(); y++ {
                for x := 0; x < img.Rect.Dx(); x++ {
                        i := img.PixOffset(x, y)
                        oi := out.PixOffset(x, y)
                        for c := 0; c < 3; c++ {
                                orig := float64(img.Pix[i+c])
                                blur := float64(blurred.Pix[i+c])
                                v := orig + (orig-blur)*strength
                                out.Pix[oi+c] = clamp255(int32(v))
                        }
                        out.Pix[oi+3] = img.Pix[i+3]
                }
        }
        return EncodePNG(out)
}

// applyVignette — darkens edges, amount controls strength.
func applyVignette(img *image.NRGBA, amount int) ([]byte, error) {
        w, h := img.Rect.Dx(), img.Rect.Dy()
        cx, cy := float64(w)/2, float64(h)/2
        maxDist := math.Sqrt(cx*cx + cy*cy)
        strength := float64(amount) / 100.0
        out := image.NewNRGBA(img.Rect)
        for y := 0; y < h; y++ {
                for x := 0; x < w; x++ {
                        i := img.PixOffset(x, y)
                        dx := float64(x) - cx
                        dy := float64(y) - cy
                        dist := math.Sqrt(dx*dx + dy*dy)
                        factor := 1.0 - strength*(dist/maxDist)
                        if factor < 0 {
                                factor = 0
                        }
                        out.Pix[i] = uint8(float64(img.Pix[i]) * factor)
                        out.Pix[i+1] = uint8(float64(img.Pix[i+1]) * factor)
                        out.Pix[i+2] = uint8(float64(img.Pix[i+2]) * factor)
                        out.Pix[i+3] = img.Pix[i+3]
                }
        }
        return EncodePNG(out)
}

// applyGrain — film grain noise.
func applyGrain(img *image.NRGBA, amount int) ([]byte, error) {
        noiseLevel := float64(amount) / 100.0 * 80 // 0..80
        seed := int64(12345)
        w, h := img.Rect.Dx(), img.Rect.Dy()
        out := image.NewNRGBA(img.Rect)
        copy(out.Pix, img.Pix)
        for y := 0; y < h; y++ {
                for x := 0; x < w; x++ {
                        i := out.PixOffset(x, y)
                        if out.Pix[i+3] == 0 {
                                continue
                        }
                        noise := (randFloat(&seed) - 0.5) * noiseLevel
                        for c := 0; c < 3; c++ {
                                v := float64(out.Pix[i+c]) + noise
                                out.Pix[i+c] = clamp255(int32(v))
                        }
                }
        }
        return EncodePNG(out)
}

// applyThreshold — binarization: pixels above threshold → white, below → black.
func applyThreshold(img *image.NRGBA, amount int) ([]byte, error) {
        threshold := amount * 255 / 100 // 0-100 → 0-255
        out := image.NewNRGBA(img.Rect)
        for y := 0; y < img.Rect.Dy(); y++ {
                for x := 0; x < img.Rect.Dx(); x++ {
                        i := img.PixOffset(x, y)
                        lum := 0.299*float64(img.Pix[i]) + 0.587*float64(img.Pix[i+1]) + 0.114*float64(img.Pix[i+2])
                        var v uint8
                        if int(lum) > threshold {
                                v = 255
                        }
                        out.Pix[i] = v
                        out.Pix[i+1] = v
                        out.Pix[i+2] = v
                        out.Pix[i+3] = img.Pix[i+3]
                }
        }
        return EncodePNG(out)
}

// applyDither — Floyd-Steinberg dithering to black & white.
func applyDither(img *image.NRGBA, amount int) ([]byte, error) {
        w, h := img.Rect.Dx(), img.Rect.Dy()
        // Work on a grayscale buffer with error diffusion.
        gray := make([][]float64, h)
        for y := 0; y < h; y++ {
                gray[y] = make([]float64, w)
                for x := 0; x < w; x++ {
                        i := img.PixOffset(x, y)
                        gray[y][x] = 0.299*float64(img.Pix[i]) + 0.587*float64(img.Pix[i+1]) + 0.114*float64(img.Pix[i+2])
                }
        }
        out := image.NewNRGBA(img.Rect)
        for y := 0; y < h; y++ {
                for x := 0; x < w; x++ {
                        old := gray[y][x]
                        var newVal float64
                        if old > 127 {
                                newVal = 255
                        }
                        err := old - newVal
                        // Floyd-Steinberg diffusion
                        if x+1 < w {
                                gray[y][x+1] += err * 7 / 16
                        }
                        if y+1 < h {
                                if x > 0 {
                                        gray[y+1][x-1] += err * 3 / 16
                                }
                                gray[y+1][x] += err * 5 / 16
                                if x+1 < w {
                                        gray[y+1][x+1] += err * 1 / 16
                                }
                        }
                        oi := out.PixOffset(x, y)
                        v := uint8(newVal)
                        out.Pix[oi] = v
                        out.Pix[oi+1] = v
                        out.Pix[oi+2] = v
                        out.Pix[oi+3] = img.Pix[oi+3]
                }
        }
        return EncodePNG(out)
}

// applyRotate — rotate by angle degrees (0-360).
func applyRotate(img *image.NRGBA, angle float64) ([]byte, error) {
        w, h := img.Rect.Dx(), img.Rect.Dy()
        rad := angle * math.Pi / 180
        cos := math.Cos(rad)
        sin := math.Sin(rad)

        // New bounding box.
        newW := int(math.Abs(float64(w)*cos) + math.Abs(float64(h)*sin) + 0.5)
        newH := int(math.Abs(float64(w)*sin) + math.Abs(float64(h)*cos) + 0.5)
        if newW < 1 {
                newW = 1
        }
        if newH < 1 {
                newH = 1
        }

        out := image.NewNRGBA(image.Rect(0, 0, newW, newH))
        cx, cy := float64(newW)/2, float64(newH)/2
        srcCX, srcCY := float64(w)/2, float64(h)/2

        for y := 0; y < newH; y++ {
                for x := 0; x < newW; x++ {
                        // Inverse rotation.
                        dx := float64(x) - cx
                        dy := float64(y) - cy
                        sx := int(dx*cos+dy*sin + srcCX)
                        sy := int(-dx*sin+dy*cos + srcCY)
                        if sx >= 0 && sx < w && sy >= 0 && sy < h {
                                si := img.PixOffset(sx, sy)
                                oi := out.PixOffset(x, y)
                                out.Pix[oi] = img.Pix[si]
                                out.Pix[oi+1] = img.Pix[si+1]
                                out.Pix[oi+2] = img.Pix[si+2]
                                out.Pix[oi+3] = img.Pix[si+3]
                        }
                }
        }
        return EncodePNG(out)
}

// applyCrop — crop to WxH+X+Y or WxH (centered).
// args[0] = "100x100+50+50" or "100x100"
func applyCrop(img *image.NRGBA, args []string) ([]byte, error) {
        if len(args) == 0 {
                return nil, fmt.Errorf("crop: need size, e.g. /filters crop 100x100+50+50")
        }
        spec := args[0]
        var cw, ch, cx, cy int
        if n, _ := fmt.Sscanf(spec, "%dx%d+%d+%d", &cw, &ch, &cx, &cy); n >= 2 {
                // Parsed WxH+X+Y or WxH
        } else {
                return nil, fmt.Errorf("crop: invalid spec %q (use WxH+X+Y)", spec)
        }
        w, h := img.Rect.Dx(), img.Rect.Dy()
        if cx+cw > w {
                cw = w - cx
        }
        if cy+ch > h {
                ch = h - cy
        }
        if cw <= 0 || ch <= 0 {
                return nil, fmt.Errorf("crop: invalid region %dx%d at (%d,%d)", cw, ch, cx, cy)
        }
        out := image.NewNRGBA(image.Rect(0, 0, cw, ch))
        for y := 0; y < ch; y++ {
                for x := 0; x < cw; x++ {
                        si := img.PixOffset(cx+x, cy+y)
                        oi := out.PixOffset(x, y)
                        out.Pix[oi] = img.Pix[si]
                        out.Pix[oi+1] = img.Pix[si+1]
                        out.Pix[oi+2] = img.Pix[si+2]
                        out.Pix[oi+3] = img.Pix[si+3]
                }
        }
        return EncodePNG(out)
}

// applyWatermark — text watermark in bottom-right corner.
func applyWatermark(img *image.NRGBA, text string, amount int) ([]byte, error) {
        if text == "" {
                text = "Pweper Bot"
        }
        opacity := uint8(amount * 255 / 100)
        if opacity < 30 {
                opacity = 30
        }
        out := image.NewNRGBA(img.Rect)
        copy(out.Pix, img.Pix)

        // Draw text in bottom-right with semi-transparent white.
        // Simple bitmap font (5×7) for ASCII chars.
        w, h := img.Rect.Dx(), img.Rect.Dy()
        fontW, fontH := 6, 8
        textW := len(text) * fontW
        startX := w - textW - 10
        startY := h - fontH - 10
        if startX < 0 {
                startX = 0
        }
        if startY < 0 {
                startY = 0
        }

        for i, ch := range text {
                glyph := getGlyph(ch)
                for gy := 0; gy < 7; gy++ {
                        for gx := 0; gx < 5; gx++ {
                                if glyph[gy]&(1<<(4-gx)) != 0 {
                                        px := startX + i*fontW + gx
                                        py := startY + gy
                                        if px >= 0 && px < w && py >= 0 && py < h {
                                                oi := out.PixOffset(px, py)
                                                // Blend white with opacity.
                                                out.Pix[oi] = blendU8(255, out.Pix[oi], opacity)
                                                out.Pix[oi+1] = blendU8(255, out.Pix[oi+1], opacity)
                                                out.Pix[oi+2] = blendU8(255, out.Pix[oi+2], opacity)
                                        }
                                }
                        }
                }
        }
        return EncodePNG(out)
}

// applyCollage — creates a 2×2 collage from one image (4 quadrants swapped).
func applyCollage(img *image.NRGBA, amount int) ([]byte, error) {
        w, h := img.Rect.Dx(), img.Rect.Dy()
        hw, hh := w/2, h/2
        out := image.NewNRGBA(image.Rect(0, 0, w, h))
        // Swap quadrants: TL↔BR, TR↔BL.
        quadrants := [4][2]int{{0, 0}, {hw, 0}, {0, hh}, {hw, hh}}
        swaps := [4]int{3, 2, 1, 0} // dest = swaps[src]
        for sq := 0; sq < 4; sq++ {
                dq := swaps[sq]
                sx, sy := quadrants[sq][0], quadrants[sq][1]
                dx, dy := quadrants[dq][0], quadrants[dq][1]
                for y := 0; y < hh; y++ {
                        for x := 0; x < hw; x++ {
                                si := img.PixOffset(sx+x, sy+y)
                                di := out.PixOffset(dx+x, dy+y)
                                out.Pix[di] = img.Pix[si]
                                out.Pix[di+1] = img.Pix[si+1]
                                out.Pix[di+2] = img.Pix[si+2]
                                out.Pix[di+3] = img.Pix[si+3]
                        }
                }
        }
        return EncodePNG(out)
}

// ── helpers ────────────────────────────────────────────────────────────────

// randFloat returns a pseudo-random float64 in [0, 1) using a simple LCG.
// Used by /grain for noise.
func randFloat(seed *int64) float64 {
        *seed = (*seed*1103515245 + 12345) & 0x7FFFFFFF
        return float64(*seed) / float64(0x7FFFFFFF)
}

func parseFloat(s string) (float64, error) {
        var f float64
        _, err := fmt.Sscanf(s, "%f", &f)
        return f, err
}

// blendU8 is already defined in imaging.go — reuse it.
// (Deleted duplicate here.)

// getGlyph returns a 5×7 bitmap for one ASCII character (rows are uint8, bit 4=leftmost).
var glyphs = map[rune][7]uint8{
        ' ': {0, 0, 0, 0, 0, 0, 0},
        'A': {0b01110, 0b10001, 0b10001, 0b11111, 0b10001, 0b10001, 0b10001},
        'B': {0b11110, 0b10001, 0b10001, 0b11110, 0b10001, 0b10001, 0b11110},
        'C': {0b01110, 0b10001, 0b10000, 0b10000, 0b10000, 0b10001, 0b01110},
        'D': {0b11110, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b11110},
        'E': {0b11111, 0b10000, 0b10000, 0b11110, 0b10000, 0b10000, 0b11111},
        'F': {0b11111, 0b10000, 0b10000, 0b11110, 0b10000, 0b10000, 0b10000},
        'G': {0b01110, 0b10001, 0b10000, 0b10111, 0b10001, 0b10001, 0b01110},
        'H': {0b10001, 0b10001, 0b10001, 0b11111, 0b10001, 0b10001, 0b10001},
        'I': {0b01110, 0b00100, 0b00100, 0b00100, 0b00100, 0b00100, 0b01110},
        'K': {0b10001, 0b10010, 0b10100, 0b11000, 0b10100, 0b10010, 0b10001},
        'L': {0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b11111},
        'M': {0b10001, 0b11011, 0b10101, 0b10101, 0b10001, 0b10001, 0b10001},
        'N': {0b10001, 0b11001, 0b10101, 0b10011, 0b10001, 0b10001, 0b10001},
        'O': {0b01110, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b01110},
        'P': {0b11110, 0b10001, 0b10001, 0b11110, 0b10000, 0b10000, 0b10000},
        'R': {0b11110, 0b10001, 0b10001, 0b11110, 0b10100, 0b10010, 0b10001},
        'S': {0b01111, 0b10000, 0b10000, 0b01110, 0b00001, 0b00001, 0b11110},
        'T': {0b11111, 0b00100, 0b00100, 0b00100, 0b00100, 0b00100, 0b00100},
        'U': {0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b01110},
        'V': {0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b01010, 0b00100},
        'W': {0b10001, 0b10001, 0b10001, 0b10101, 0b10101, 0b11011, 0b10001},
        'X': {0b10001, 0b10001, 0b01010, 0b00100, 0b01010, 0b10001, 0b10001},
        'Y': {0b10001, 0b10001, 0b01010, 0b00100, 0b00100, 0b00100, 0b00100},
        'Z': {0b11111, 0b00001, 0b00010, 0b00100, 0b01000, 0b10000, 0b11111},
}

func getGlyph(ch rune) [7]uint8 {
        if g, ok := glyphs[ch]; ok {
                return g
        }
        if g, ok := glyphs[ch-'a'+'A']; ok {
                return g
        }
        // Default: dot
        return [7]uint8{0, 0, 0, 0, 0, 0, 0b00100}
}

// Ensure color import is used.
var _ = color.NRGBA{}
