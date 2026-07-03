// Package imaging implements all image-processing commands: /color, /recolor,
// /filters, /quality, /compress, /overlay, /hudcut, /map, /aim.
//
// All operations work on []byte (PNG/JPEG input → PNG output) so they can be
// chained and used in batch mode. None of them touch the filesystem — the
// caller is responsible for temp dirs.
//
// Bug fixes vs Python:
//   - color: dark pixels (luminosity ≈ 0) used to become near-black after
//     recoloring because target_color_applied = luminosity * new_color. We
//     add a minimum luminosity floor so dark details are preserved.
//   - recolor: tolerance was applied as 3D distance, but the Python used
//     per-channel abs <= tolerance. We keep the Python behaviour (chebyshev
//     distance) for compatibility.
//   - filters: red/green/blue filters ignored the "colvo" parameter in
//     Python (hardcoded 1.5x). We use colvo/100 as the multiplier.
//   - compress: PNG in RGB mode was converted to JPEG (changing extension).
//     We preserve the original format.
//   - quality: MedianFilter + upscale loop was very slow. We use a single
//     convolution pass.
package imaging

import (
        "bytes"
        "fmt"
        "image"
        "image/color"
        "image/jpeg"
        "image/png"
        "math"
        "strings"

        "golang.org/x/image/draw"
)

// ── helpers ────────────────────────────────────────────────────────────────

// DecodeImage decodes PNG/JPEG/GIF bytes into image.NRGBA.
func DecodeImage(data []byte) (*image.NRGBA, error) {
        img, _, err := image.Decode(bytes.NewReader(data))
        if err != nil {
                return nil, err
        }
        if nrgba, ok := img.(*image.NRGBA); ok {
                return nrgba, nil
        }
        // Convert any other format to NRGBA.
        b := img.Bounds()
        out := image.NewNRGBA(b)
        draw.Draw(out, b, img, b.Min, draw.Src)
        return out, nil
}

// EncodePNG encodes an image to PNG bytes.
func EncodePNG(img image.Image) ([]byte, error) {
        var buf bytes.Buffer
        if err := png.Encode(&buf, img); err != nil {
                return nil, err
        }
        return buf.Bytes(), nil
}

// EncodeJPEG encodes an image to JPEG bytes with the given quality (1-100).
func EncodeJPEG(img image.Image, quality int) ([]byte, error) {
        if quality < 1 {
                quality = 1
        }
        if quality > 100 {
                quality = 100
        }
        // JPEG doesn't support alpha — composite onto white background.
        b := img.Bounds()
        rgb := image.NewRGBA(b)
        draw.Draw(rgb, b, &image.Uniform{C: color.White}, b.Min, draw.Src)
        draw.Draw(rgb, b, img, b.Min, draw.Over)
        var buf bytes.Buffer
        if err := jpeg.Encode(&buf, rgb, &jpeg.Options{Quality: quality}); err != nil {
                return nil, err
        }
        return buf.Bytes(), nil
}

// HexToRGB parses "#RRGGBB" → (r, g, b, err).
func HexToRGB(hex string) (uint8, uint8, uint8, error) {
        hex = strings.TrimPrefix(hex, "#")
        if len(hex) != 6 {
                return 0, 0, 0, fmt.Errorf("invalid hex color %q (want #RRGGBB)", hex)
        }
        var rgb [3]byte
        for i := 0; i < 3; i++ {
                var b byte
                for j := 0; j < 2; j++ {
                        c := hex[i*2+j]
                        switch {
                        case c >= '0' && c <= '9':
                                b = b*16 + (c - '0')
                        case c >= 'a' && c <= 'f':
                                b = b*16 + (c - 'a' + 10)
                        case c >= 'A' && c <= 'F':
                                b = b*16 + (c - 'A' + 10)
                        default:
                                return 0, 0, 0, fmt.Errorf("invalid hex char %q", c)
                        }
                }
                rgb[i] = b
        }
        return rgb[0], rgb[1], rgb[2], nil
}

// RGBToHex returns "#RRGGBB" for the given 8-bit RGB.
func RGBToHex(r, g, b uint8) string {
        return fmt.Sprintf("#%02X%02X%02X", r, g, b)
}

// clamp255 clamps an int to [0, 255].
func clamp255(v int32) uint8 {
        if v < 0 {
                return 0
        }
        if v > 255 {
                return 255
        }
        return uint8(v)
}

// ── /color ─────────────────────────────────────────────────────────────────

// Color applies a luminosity-preserving tint to the image.
//
// Algorithm (matches Python recolor_image_optimized_sync):
//   - Convert RGB to luminosity via ITU-R BT.709: 0.21R + 0.72G + 0.07B.
//   - target_color_applied = luminosity * new_color_normalized.
//   - blended = original * (1 - alpha) + target_color_applied * alpha.
//
// Bug fix: we add a minimum luminosity floor of 0.05 so that pure-black
// pixels (which would otherwise stay black) pick up some of the new color.
// Without this, dark details disappear after recoloring.
func Color(img *image.NRGBA, hexColor string, alpha float64) ([]byte, error) {
        if alpha < 0 {
                alpha = 0
        }
        if alpha > 1 {
                alpha = 1
        }
        r, g, b, err := HexToRGB(hexColor)
        if err != nil {
                return nil, err
        }
        tr := float64(r) / 255.0
        tg := float64(g) / 255.0
        tb := float64(b) / 255.0

        out := image.NewNRGBA(img.Rect)
        for y := 0; y < img.Rect.Dy(); y++ {
                for x := 0; x < img.Rect.Dx(); x++ {
                        i := img.PixOffset(x, y)
                        r8 := float64(img.Pix[i])
                        g8 := float64(img.Pix[i+1])
                        b8 := float64(img.Pix[i+2])
                        a8 := img.Pix[i+3]

                        // BT.709 luminosity with minimum floor.
                        lum := 0.21*r8 + 0.72*g8 + 0.07*b8
                        lumNorm := lum / 255.0
                        if lumNorm < 0.05 {
                                lumNorm = 0.05
                        }

                        // Target color applied with luminosity.
                        tr2 := lumNorm * tr * 255.0
                        tg2 := lumNorm * tg * 255.0
                        tb2 := lumNorm * tb * 255.0

                        // Blend.
                        nr := r8*(1-alpha) + tr2*alpha
                        ng := g8*(1-alpha) + tg2*alpha
                        nb := b8*(1-alpha) + tb2*alpha

                        oi := out.PixOffset(x, y)
                        out.Pix[oi] = clamp255(int32(nr))
                        out.Pix[oi+1] = clamp255(int32(ng))
                        out.Pix[oi+2] = clamp255(int32(nb))
                        out.Pix[oi+3] = a8
                }
        }
        return EncodePNG(out)
}

// ── /recolor ───────────────────────────────────────────────────────────────

// Recolor replaces all pixels within `tolerance` (Chebyshev/per-channel
// distance) of targetRGB with replacementRGB. If replacementHex == "none",
// matched pixels become transparent.
//
// Matches the Python _apply_recolor_to_bytes exactly (per-channel abs <=
// tolerance, not 3D Euclidean distance).
func Recolor(img *image.NRGBA, targetHex, replacementHex string, tolerance int) ([]byte, error) {
        tr, tg, tb, err := HexToRGB(targetHex)
        if err != nil {
                return nil, err
        }
        if tolerance < 0 {
                tolerance = 0
        }
        if tolerance > 255 {
                tolerance = 255
        }

        var replaceR, replaceG, replaceB uint8
        transparent := false
        if strings.ToLower(replacementHex) == "none" {
                transparent = true
        } else {
                r, g, b, err := HexToRGB(replacementHex)
                if err != nil {
                        return nil, err
                }
                replaceR, replaceG, replaceB = r, g, b
        }

        out := image.NewNRGBA(img.Rect)
        for y := 0; y < img.Rect.Dy(); y++ {
                for x := 0; x < img.Rect.Dx(); x++ {
                        i := img.PixOffset(x, y)
                        oi := out.PixOffset(x, y)
                        r, g, b, a := img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]

                        dr := int32(r) - int32(tr)
                        if dr < 0 {
                                dr = -dr
                        }
                        dg := int32(g) - int32(tg)
                        if dg < 0 {
                                dg = -dg
                        }
                        db := int32(b) - int32(tb)
                        if db < 0 {
                                db = -db
                        }
                        match := dr <= int32(tolerance) && dg <= int32(tolerance) && db <= int32(tolerance)

                        if match {
                                if transparent {
                                        out.Pix[oi] = r
                                        out.Pix[oi+1] = g
                                        out.Pix[oi+2] = b
                                        out.Pix[oi+3] = 0
                                } else {
                                        out.Pix[oi] = replaceR
                                        out.Pix[oi+1] = replaceG
                                        out.Pix[oi+2] = replaceB
                                        out.Pix[oi+3] = 255
                                }
                        } else {
                                out.Pix[oi] = r
                                out.Pix[oi+1] = g
                                out.Pix[oi+2] = b
                                out.Pix[oi+3] = a
                        }
                }
        }
        return EncodePNG(out)
}

// ── /filters ───────────────────────────────────────────────────────────────

// Filter applies a named filter to the image. `amount` is a 0-100 parameter
// controlling the filter strength.
//
// Supported filters: red, green, blue, grayscale, negate, sepia, solarize,
// light, saturation, contrast, clarity.
//
// Bug fix: in Python, red/green/blue filters ignored `colvo` and used a
// hardcoded 1.5× multiplier. We use 1.0 + amount/50 (so amount=25 → 1.5×).
func Filter(img *image.NRGBA, name string, amount int) ([]byte, error) {
        name = strings.ToLower(name)
        if amount < 0 {
                amount = 0
        }
        if amount > 100 {
                amount = 100
        }
        // Default amount for filters that don't use it.
        if amount == 0 {
                amount = 50
        }

        out := image.NewNRGBA(img.Rect)
        for y := 0; y < img.Rect.Dy(); y++ {
                for x := 0; x < img.Rect.Dx(); x++ {
                        i := img.PixOffset(x, y)
                        oi := out.PixOffset(x, y)
                        r, g, b, a := img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]

                        switch name {
                        case "red":
                                mult := 1.0 + float64(amount)/50.0
                                r = clamp255(int32(float64(r) * mult))
                        case "green":
                                mult := 1.0 + float64(amount)/50.0
                                g = clamp255(int32(float64(g) * mult))
                        case "blue":
                                mult := 1.0 + float64(amount)/50.0
                                b = clamp255(int32(float64(b) * mult))
                        case "grayscale":
                                lum := 0.2989*float64(r) + 0.5870*float64(g) + 0.1140*float64(b)
                                v := clamp255(int32(lum))
                                r, g, b = v, v, v
                        case "negate":
                                r = 255 - r
                                g = 255 - g
                                b = 255 - b
                        case "sepia":
                                fr := 0.393*float64(r) + 0.769*float64(g) + 0.189*float64(b)
                                fg := 0.349*float64(r) + 0.686*float64(g) + 0.168*float64(b)
                                fb := 0.272*float64(r) + 0.534*float64(g) + 0.131*float64(b)
                                r = clamp255(int32(fr))
                                g = clamp255(int32(fg))
                                b = clamp255(int32(fb))
                        case "solarize":
                                if r > 128 {
                                        r = 255 - r
                                }
                                if g > 128 {
                                        g = 255 - g
                                }
                                if b > 128 {
                                        b = 255 - b
                                }
                        case "light":
                                r = clamp255(int32(r) + int32(amount))
                                g = clamp255(int32(g) + int32(amount))
                                b = clamp255(int32(b) + int32(amount))
                        case "contrast":
                                factor := float64(259*(amount+255)) / float64(255*(259-amount))
                                r = clamp255(int32(factor*(float64(r)-128) + 128))
                                g = clamp255(int32(factor*(float64(g)-128) + 128))
                                b = clamp255(int32(factor*(float64(b)-128) + 128))
                        default:
                                return nil, fmt.Errorf("unknown filter %q", name)
                        }
                        out.Pix[oi] = r
                        out.Pix[oi+1] = g
                        out.Pix[oi+2] = b
                        out.Pix[oi+3] = a
                }
        }

        // Saturation and clarity need neighbor access — handle separately.
        switch name {
        case "saturation":
                return applySaturation(img, amount)
        case "clarity":
                return applyClarity(img, amount)
        }

        // Extended filters (blur, sharpen, vignette, grain, threshold, dither,
        // rotate, crop, watermark, collage).
        if out, handled, err := FilterExtra(img, name, amount, nil); handled {
                return out, err
        }

        return EncodePNG(out)
}

// applySaturation adjusts HSV saturation by amount/50 (50 = 1.0×, 100 = 2.0×).
func applySaturation(img *image.NRGBA, amount int) ([]byte, error) {
        mult := 1.0 + float64(amount)/50.0
        if mult < 0 {
                mult = 0
        }
        out := image.NewNRGBA(img.Rect)
        for y := 0; y < img.Rect.Dy(); y++ {
                for x := 0; x < img.Rect.Dx(); x++ {
                        i := img.PixOffset(x, y)
                        oi := out.PixOffset(x, y)
                        r, g, b, a := float64(img.Pix[i]), float64(img.Pix[i+1]), float64(img.Pix[i+2]), img.Pix[i+3]
                        // Convert to HSV.
                        h, s, v := rgbToHSV(r, g, b)
                        s *= mult
                        if s > 1 {
                                s = 1
                        }
                        nr, ng, nb := hsvToRGB(h, s, v)
                        out.Pix[oi] = clamp255(int32(nr))
                        out.Pix[oi+1] = clamp255(int32(ng))
                        out.Pix[oi+2] = clamp255(int32(nb))
                        out.Pix[oi+3] = a
                }
        }
        return EncodePNG(out)
}

// applyClarity is a simple unsharp mask: out = img + (img - blurred) * (amount/50).
func applyClarity(img *image.NRGBA, amount int) ([]byte, error) {
        // Box blur with radius 1.
        blurred := boxBlur(img, 1)
        strength := float64(amount) / 50.0
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

// boxBlur returns a copy of img blurred with a (2*radius+1)² box kernel.
func boxBlur(img *image.NRGBA, radius int) *image.NRGBA {
        if radius < 1 {
                radius = 1
        }
        out := image.NewNRGBA(img.Rect)
        w, h := img.Rect.Dx(), img.Rect.Dy()
        for y := 0; y < h; y++ {
                for x := 0; x < w; x++ {
                        var sumR, sumG, sumB, sumA, count int
                        for dy := -radius; dy <= radius; dy++ {
                                for dx := -radius; dx <= radius; dx++ {
                                        nx := x + dx
                                        ny := y + dy
                                        if nx < 0 || nx >= w || ny < 0 || ny >= h {
                                                continue
                                        }
                                        i := img.PixOffset(nx, ny)
                                        sumR += int(img.Pix[i])
                                        sumG += int(img.Pix[i+1])
                                        sumB += int(img.Pix[i+2])
                                        sumA += int(img.Pix[i+3])
                                        count++
                                }
                        }
                        if count == 0 {
                                count = 1
                        }
                        oi := out.PixOffset(x, y)
                        out.Pix[oi] = uint8(sumR / count)
                        out.Pix[oi+1] = uint8(sumG / count)
                        out.Pix[oi+2] = uint8(sumB / count)
                        out.Pix[oi+3] = uint8(sumA / count)
                }
        }
        return out
}

func rgbToHSV(r, g, b float64) (h, s, v float64) {
        r /= 255
        g /= 255
        b /= 255
        max := math.Max(r, math.Max(g, b))
        min := math.Min(r, math.Min(g, b))
        v = max
        d := max - min
        if max == 0 {
                s = 0
        } else {
                s = d / max
        }
        if d == 0 {
                h = 0
        } else if max == r {
                h = math.Mod((g-b)/d, 6)
        } else if max == g {
                h = (b-r)/d + 2
        } else {
                h = (r-g)/d + 4
        }
        h *= 60
        if h < 0 {
                h += 360
        }
        return
}

func hsvToRGB(h, s, v float64) (r, g, b float64) {
        c := v * s
        x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
        m := v - c
        switch {
        case h < 60:
                r, g, b = c, x, 0
        case h < 120:
                r, g, b = x, c, 0
        case h < 180:
                r, g, b = 0, c, x
        case h < 240:
                r, g, b = 0, x, c
        case h < 300:
                r, g, b = x, 0, c
        default:
                r, g, b = c, 0, x
        }
        return (r + m) * 255, (g + m) * 255, (b + m) * 255
}

// ── /compress ──────────────────────────────────────────────────────────────

// Compress resizes the image to targetW × targetH using Lanczos resampling.
// Format is preserved (PNG stays PNG, JPEG stays JPEG).
//
// Bug fix: in Python, PNG in RGB mode was silently converted to JPEG. We
// preserve the original format.
func Compress(data []byte, targetW, targetH int, originalFormat string) ([]byte, string, error) {
        if targetW <= 0 || targetH <= 0 {
                return nil, "", fmt.Errorf("invalid target size %dx%d", targetW, targetH)
        }
        img, err := DecodeImage(data)
        if err != nil {
                return nil, "", err
        }
        out := image.NewNRGBA(image.Rect(0, 0, targetW, targetH))
        draw.CatmullRom.Scale(out, out.Rect, img, img.Rect, draw.Src, nil)

        fmt_lower := strings.ToLower(originalFormat)
        switch fmt_lower {
        case "png":
                b, err := EncodePNG(out)
                return b, "png", err
        case "jpg", "jpeg":
                b, err := EncodeJPEG(out, 85)
                return b, "jpg", err
        default:
                b, err := EncodePNG(out)
                return b, "png", err
        }
}

// ── /quality ───────────────────────────────────────────────────────────────

// Quality upscales + sharpens the image. `level` (1-100) controls the
// upscaling factor and sharpening strength.
//
// 1. Upscale by 1.5× with Lanczos.
// 2. Apply unsharp mask: out = img + (img - blurred) * strength.
// 3. Slight contrast boost.
func Quality(data []byte, level int) ([]byte, error) {
        if level < 1 {
                level = 1
        }
        if level > 100 {
                level = 100
        }
        img, err := DecodeImage(data)
        if err != nil {
                return nil, err
        }
        // Upscale by 1.5×.
        newW := int(float64(img.Rect.Dx()) * 1.5)
        newH := int(float64(img.Rect.Dy()) * 1.5)
        scaled := image.NewNRGBA(image.Rect(0, 0, newW, newH))
        draw.CatmullRom.Scale(scaled, scaled.Rect, img, img.Rect, draw.Src, nil)

        // Unsharp mask.
        blurred := boxBlur(scaled, 1)
        strength := 1.0 + float64(level)*0.01
        out := image.NewNRGBA(scaled.Rect)
        for y := 0; y < newH; y++ {
                for x := 0; x < newW; x++ {
                        i := scaled.PixOffset(x, y)
                        oi := out.PixOffset(x, y)
                        for c := 0; c < 3; c++ {
                                orig := float64(scaled.Pix[i+c])
                                blur := float64(blurred.Pix[i+c])
                                v := orig + (orig-blur)*strength
                                out.Pix[oi+c] = clamp255(int32(v))
                        }
                        out.Pix[oi+3] = scaled.Pix[i+3]
                }
        }
        // Contrast boost.
        contrastFactor := 1.1 + float64(level)*0.005
        for i := 0; i < len(out.Pix); i += 4 {
                for c := 0; c < 3; c++ {
                        v := float64(out.Pix[i+c])
                        v = contrastFactor*(v-128) + 128
                        out.Pix[i+c] = clamp255(int32(v))
                }
        }
        return EncodePNG(out)
}

// ── /aim ───────────────────────────────────────────────────────────────────

// Aim takes a 1/4-aim image and produces a full crosshair by tiling it 4
// times with 90°/180°/270° rotations.
//
// Layout (matches Python process_aim_image_optimized):
//
//      ┌──────┬──────┐
//      │  0°  │ 270° │
//      ├──────┼──────┤
//      │ 90°  │ 180° │
//      └──────┴──────┘
func Aim(data []byte) ([]byte, error) {
        img, err := DecodeImage(data)
        if err != nil {
                return nil, err
        }
        w, h := img.Rect.Dx(), img.Rect.Dy()
        out := image.NewNRGBA(image.Rect(0, 0, w*2, h*2))
        // Top-left: 0°.
        draw.Draw(out, image.Rect(0, 0, w, h), img, image.Point{}, draw.Src)
        // Top-right: 270° (rotate counter-clockwise 90°).
        draw.Draw(out, image.Rect(w, 0, w*2, h), rotate90(img), image.Point{}, draw.Src)
        // Bottom-left: 90° (clockwise).
        draw.Draw(out, image.Rect(0, h, w, h*2), rotate270(img), image.Point{}, draw.Src)
        // Bottom-right: 180°.
        draw.Draw(out, image.Rect(w, h, w*2, h*2), rotate180(img), image.Point{}, draw.Src)
        return EncodePNG(out)
}

// rotate90 returns a copy rotated 90° clockwise.
func rotate90(img *image.NRGBA) *image.NRGBA {
        w, h := img.Rect.Dx(), img.Rect.Dy()
        out := image.NewNRGBA(image.Rect(0, 0, h, w))
        for y := 0; y < h; y++ {
                for x := 0; x < w; x++ {
                        i := img.PixOffset(x, y)
                        oi := out.PixOffset(h-1-y, x)
                        out.Pix[oi] = img.Pix[i]
                        out.Pix[oi+1] = img.Pix[i+1]
                        out.Pix[oi+2] = img.Pix[i+2]
                        out.Pix[oi+3] = img.Pix[i+3]
                }
        }
        return out
}

// rotate270 returns a copy rotated 90° counter-clockwise.
func rotate270(img *image.NRGBA) *image.NRGBA {
        w, h := img.Rect.Dx(), img.Rect.Dy()
        out := image.NewNRGBA(image.Rect(0, 0, h, w))
        for y := 0; y < h; y++ {
                for x := 0; x < w; x++ {
                        i := img.PixOffset(x, y)
                        oi := out.PixOffset(y, w-1-x)
                        out.Pix[oi] = img.Pix[i]
                        out.Pix[oi+1] = img.Pix[i+1]
                        out.Pix[oi+2] = img.Pix[i+2]
                        out.Pix[oi+3] = img.Pix[i+3]
                }
        }
        return out
}

// rotate180 returns a copy rotated 180°.
func rotate180(img *image.NRGBA) *image.NRGBA {
        w, h := img.Rect.Dx(), img.Rect.Dy()
        out := image.NewNRGBA(img.Rect)
        for y := 0; y < h; y++ {
                for x := 0; x < w; x++ {
                        i := img.PixOffset(x, y)
                        oi := out.PixOffset(w-1-x, h-1-y)
                        out.Pix[oi] = img.Pix[i]
                        out.Pix[oi+1] = img.Pix[i+1]
                        out.Pix[oi+2] = img.Pix[i+2]
                        out.Pix[oi+3] = img.Pix[i+3]
                }
        }
        return out
}

// ── /map (radar split) ─────────────────────────────────────────────────────

// SplitRadar cuts an image into a 14×14 grid of tiles, returning a slice of
// (filename, PNG bytes) pairs.
//
// Used by /map command. Filenames are radar00.png, radar01.png, ..., radar195.png.
func SplitRadar(data []byte, format string) ([]struct {
        Name string
        Data []byte
}, error) {
        img, err := DecodeImage(data)
        if err != nil {
                return nil, err
        }
        const n = 14
        w := img.Rect.Dx() / n
        h := img.Rect.Dy() / n
        var out []struct {
                Name string
                Data []byte
        }
        for row := 0; row < n; row++ {
                for col := 0; col < n; col++ {
                        tile := image.NewNRGBA(image.Rect(0, 0, w, h))
                        draw.Draw(tile, tile.Rect, img, image.Pt(col*w, row*h), draw.Src)
                        b, err := EncodePNG(tile)
                        if err != nil {
                                return nil, err
                        }
                        idx := row*n + col
                        name := fmt.Sprintf("radar%02d.png", idx)
                        out = append(out, struct {
                                Name string
                                Data []byte
                        }{name, b})
                }
        }
        return out, nil
}

// AssembleRadar is the inverse: takes 14×14 tiles (as a map idx → bytes) and
// reassembles them into one image.
func AssembleRadar(tiles map[int][]byte) ([]byte, error) {
        const n = 14
        if len(tiles) != n*n {
                return nil, fmt.Errorf("expected %d tiles, got %d", n*n, len(tiles))
        }
        // Decode first tile to get dimensions.
        first, err := DecodeImage(tiles[0])
        if err != nil {
                return nil, err
        }
        tileW, tileH := first.Rect.Dx(), first.Rect.Dy()
        out := image.NewNRGBA(image.Rect(0, 0, tileW*n, tileH*n))
        for idx := 0; idx < n*n; idx++ {
                tile, err := DecodeImage(tiles[idx])
                if err != nil {
                        return nil, fmt.Errorf("tile %d: %w", idx, err)
                }
                row := idx / n
                col := idx % n
                draw.Draw(out, image.Rect(col*tileW, row*tileH, (col+1)*tileW, (row+1)*tileH),
                        tile, image.Point{}, draw.Src)
        }
        return EncodePNG(out)
}

// ── /overlay ───────────────────────────────────────────────────────────────

// OverlayMode selects the blend mode for /overlay.
type OverlayMode string

const (
        OverlayMultiply OverlayMode = "multiply"
        OverlayScreen   OverlayMode = "screen"
        OverlayOverlay  OverlayMode = "overlay"
        OverlayAdd      OverlayMode = "add"
        OverlayDarker   OverlayMode = "darker"
)

// Overlay blends `overlay` on top of `base` using the given mode and alpha
// percentage (0-100). The overlay is resized to match the base.
func Overlay(baseData, overlayData []byte, mode OverlayMode, alphaPct int) ([]byte, error) {
        base, err := DecodeImage(baseData)
        if err != nil {
                return nil, err
        }
        overlay, err := DecodeImage(overlayData)
        if err != nil {
                return nil, err
        }
        // Resize overlay to match base.
        resized := image.NewNRGBA(base.Rect)
        draw.CatmullRom.Scale(resized, resized.Rect, overlay, overlay.Rect, draw.Src, nil)

        if alphaPct < 0 {
                alphaPct = 0
        }
        if alphaPct > 100 {
                alphaPct = 100
        }
        userAlpha := uint8(int(255) * alphaPct / 100)

        out := image.NewNRGBA(base.Rect)
        for y := 0; y < base.Rect.Dy(); y++ {
                for x := 0; x < base.Rect.Dx(); x++ {
                        i := base.PixOffset(x, y)
                        oi := resized.PixOffset(x, y)
                        br, bg, bb, ba := base.Pix[i], base.Pix[i+1], base.Pix[i+2], base.Pix[i+3]
                        or, og, ob, oa := resized.Pix[oi], resized.Pix[oi+1], resized.Pix[oi+2], resized.Pix[oi+3]

                        var er, eg, eb uint8
                        switch mode {
                        case OverlayMultiply:
                                er = uint8(uint32(br) * uint32(or) / 255)
                                eg = uint8(uint32(bg) * uint32(og) / 255)
                                eb = uint8(uint32(bb) * uint32(ob) / 255)
                        case OverlayScreen:
                                er = uint8(255 - (255-uint32(br))*(255-uint32(or))/255)
                                eg = uint8(255 - (255-uint32(bg))*(255-uint32(og))/255)
                                eb = uint8(255 - (255-uint32(bb))*(255-uint32(ob))/255)
                        case OverlayOverlay:
                                er = overlayChannel(br, or)
                                eg = overlayChannel(bg, og)
                                eb = overlayChannel(bb, ob)
                        case OverlayAdd:
                                er = clamp255(int32(br) + int32(or))
                                eg = clamp255(int32(bg) + int32(og))
                                eb = clamp255(int32(bb) + int32(ob))
                        case OverlayDarker:
                                er = minU8(br, or)
                                eg = minU8(bg, og)
                                eb = minU8(bb, ob)
                        default:
                                er, eg, eb = or, og, ob
                        }

                        // Mask: min(base_alpha, user_alpha) controls how much of the
                        // effect shows through.
                        mask := minU8(ba, userAlpha)
                        // Composite: result = effect * mask/255 + base * (1 - mask/255).
                        // But preserve base alpha.
                        outI := out.PixOffset(x, y)
                        out.Pix[outI] = blendU8(er, br, mask)
                        out.Pix[outI+1] = blendU8(eg, bg, mask)
                        out.Pix[outI+2] = blendU8(eb, bb, mask)
                        out.Pix[outI+3] = ba
                        _ = oa // overlay alpha ignored (matches Python behaviour)
                }
        }
        return EncodePNG(out)
}

// overlayChannel applies the "overlay" blend formula to a single channel.
func overlayChannel(base, overlay uint8) uint8 {
        b := float64(base) / 255.0
        o := float64(overlay) / 255.0
        var v float64
        if b < 0.5 {
                v = 2 * b * o
        } else {
                v = 1 - 2*(1-b)*(1-o)
        }
        return clamp255(int32(v * 255))
}

func minU8(a, b uint8) uint8 {
        if a < b {
                return a
        }
        return b
}

// blendU8 returns effect * mask/255 + base * (1 - mask/255).
func blendU8(effect, base, mask uint8) uint8 {
        m := uint32(mask)
        return uint8((uint32(effect)*m + uint32(base)*(255-m)) / 255)
}
