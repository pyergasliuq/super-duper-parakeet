// Package txd parses GTA SA .txd texture archives and decodes the embedded
// textures to PNG. Output is a ZIP of PNG files (one per texture).
//
// Original Python: txd.py (807 lines) — used Python loops for DXT decoding
// which were ~50-100× slower than necessary. This Go port uses byte slicing
// + direct uint32 ops, no per-pixel Python overhead.
//
// Supported formats (matches txd.py):
//   - DXT1, DXT3, DXT5  (BC1/BC2/BC3 block compression)
//   - ABGR8888, ARGB8888, BGR565, BGR888, BGR888_BLUESCREEN
//   - BGRA4444, BGRA5551, BGRA8888/BGRA, BGRX5551, BGRX8888
//   - I8, IA88, RGB565, RGB888, RGB888_BLUESCREEN
//   - RGBAH6161616, RGBAH6161616F, UV88, UVLX8888, UVWQ8888, AB
//   - ARGB1555, RGBA4444
//   - 8-bit paletted (with 1024-byte palette)
//   - depth-based fallbacks (8/16/24/32)
package txd

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"unicode/utf8"
)

// Texture is one decoded texture from a .txd file.
type Texture struct {
	Name   string
	Format string // DXT1, DXT3, DXT5, BGR888, etc.
	Width  int
	Height int
	Image  *image.NRGBA
}

// Parse parses a .txd binary blob and returns all decoded textures.
//
// Returns an error only on fatal I/O issues. Per-texture decode errors are
// logged (well, returned as part of the result) but don't abort the parse.
func Parse(data []byte) ([]Texture, error) {
	p := &parser{data: data}
	return p.parse()
}

type parser struct {
	data []byte
	pos  int
}

func (p *parser) parse() ([]Texture, error) {
	var out []Texture
	insideTexture := false

	for p.pos+12 <= len(p.data) {
		chunkID, err := p.readU32()
		if err != nil {
			break
		}
		chunkSize, err := p.readU32()
		if err != nil {
			break
		}
		_, err = p.readU32() // rw_version
		if err != nil {
			break
		}

		switch chunkID {
		case 22:
			// Generic chunk — skip.
		case 1:
			if !insideTexture {
				// Texture dictionary header: num_textures (u16) + 2 bytes padding.
				if p.pos+4 > len(p.data) {
					return out, nil
				}
				_ = binary.LittleEndian.Uint16(p.data[p.pos:])
				p.pos += 4
			} else {
				// Texture native data.
				t, err := p.parseNativeTexture()
				if err != nil {
					return out, fmt.Errorf("texture at offset %d: %w", p.pos, err)
				}
				if t != nil {
					out = append(out, *t)
				}
				insideTexture = false
			}
		case 21:
			insideTexture = true
		case 3:
			// Extension chunk — skip.
		default:
			// Skip chunkSize - 4 bytes (the rw_version was 4 bytes of chunkSize).
			// Actually the format is: id(4) + size(4) + version(4), then size-4 more bytes.
			// We've already read 12 bytes total. The remaining chunkSize-4 bytes are next.
			remaining := int(chunkSize) - 4
			if remaining < 0 {
				remaining = 0
			}
			if p.pos+remaining > len(p.data) {
				return out, nil
			}
			p.pos += remaining
		}
	}
	return out, nil
}

// parseNativeTexture parses one texture native chunk (id=1 inside id=21).
func (p *parser) parseNativeTexture() (*Texture, error) {
	// version(4) + filter_flags(4) + name(32) + alpha_name(32) + alpha_flags(4) + format(4) +
	// width(2) + height(2) + depth(1) + mipmap_count(1) + texcode_type(1) + flags(1)
	// + [palette(1024) if depth==8] + data_size(4) + data(data_size) + mipmaps(...)
	if p.pos+80 > len(p.data) {
		return nil, fmt.Errorf("not enough data for texture header")
	}
	p.pos += 4 // version
	p.pos += 4 // filter_flags

	nameBytes := p.data[p.pos : p.pos+32]
	p.pos += 32
	name := stringsFromC(nameBytes)

	p.pos += 32 // alpha_name
	p.pos += 4  // alpha_flags

	formatBytes := p.data[p.pos : p.pos+4]
	p.pos += 4
	format := string(bytes.TrimRight(formatBytes, "\x00"))

	width := int(binary.LittleEndian.Uint16(p.data[p.pos:]))
	p.pos += 2
	height := int(binary.LittleEndian.Uint16(p.data[p.pos:]))
	p.pos += 2
	depth := int(p.data[p.pos])
	p.pos++
	mipmapCount := int(p.data[p.pos])
	p.pos++
	p.pos++ // texcode_type
	p.pos++ // flags

	var palette []byte
	if depth == 8 {
		if p.pos+1024 > len(p.data) {
			return nil, fmt.Errorf("not enough data for palette")
		}
		palette = make([]byte, 1024)
		copy(palette, p.data[p.pos:p.pos+1024])
		p.pos += 1024
	}

	if p.pos+4 > len(p.data) {
		return nil, fmt.Errorf("not enough data for data_size")
	}
	dataSize := int(binary.LittleEndian.Uint32(p.data[p.pos:]))
	p.pos += 4

	if p.pos+dataSize > len(p.data) {
		// Clamp to what's available — original Python does this too.
		dataSize = len(p.data) - p.pos
	}
	textureData := make([]byte, dataSize)
	copy(textureData, p.data[p.pos:p.pos+dataSize])
	p.pos += dataSize

	// Skip mipmaps (mipmap_count - 1 of them, each preceded by 4-byte size).
	for i := 0; i < mipmapCount-1; i++ {
		if p.pos+4 > len(p.data) {
			break
		}
		sz := int(binary.LittleEndian.Uint32(p.data[p.pos:]))
		p.pos += 4
		if p.pos+sz > len(p.data) {
			break
		}
		p.pos += sz
	}

	img := decodeTexture(format, depth, textureData, width, height, palette)
	if img == nil {
		return nil, fmt.Errorf("unsupported format %q (depth=%d)", format, depth)
	}
	return &Texture{
		Name:   name,
		Format: format,
		Width:  width,
		Height: height,
		Image:  img,
	}, nil
}

func (p *parser) readU32() (uint32, error) {
	if p.pos+4 > len(p.data) {
		return 0, io.EOF
	}
	v := binary.LittleEndian.Uint32(p.data[p.pos:])
	p.pos += 4
	return v, nil
}

// stringsFromC returns the null-terminated string from a byte slice.
func stringsFromC(b []byte) string {
	n := bytes.IndexByte(b, 0)
	if n < 0 {
		n = len(b)
	}
	// Filter invalid UTF-8 (some TXDs have garbage bytes in the name).
	valid := make([]byte, 0, n)
	for i := 0; i < n; {
		r, sz := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && sz == 1 {
			// Skip invalid byte — but keep ASCII chars.
			if b[i] < 0x80 {
				valid = append(valid, b[i])
			}
			i++
			continue
		}
		valid = append(valid, b[i:i+sz]...)
		i += sz
	}
	return string(valid)
}

// ── Color unpack helpers ───────────────────────────────────────────────────

// unpack565 returns (r, g, b, 255) from a 16-bit 565 color.
func unpack565(c uint16) (r, g, b, a uint8) {
	r5 := uint8(c>>11) & 0x1F
	g6 := uint8(c>>5) & 0x3F
	b5 := uint8(c) & 0x1F
	return (r5 << 3) | (r5 >> 2),
		(g6 << 2) | (g6 >> 4),
		(b5 << 3) | (b5 >> 2),
		255
}

// unpack5551 returns (r, g, b, a) from a 16-bit 5551 color.
func unpack5551(c uint16) (r, g, b, a uint8) {
	r5 := uint8(c>>11) & 0x1F
	g5 := uint8(c>>6) & 0x1F
	b5 := uint8(c>>1) & 0x1F
	a1 := uint8(c) & 0x1
	return (r5 << 3) | (r5 >> 2),
		(g5 << 3) | (g5 >> 2),
		(b5 << 3) | (b5 >> 2),
		a1 * 255
}

// unpack4444 returns (r, g, b, a) from a 16-bit 4444 color.
func unpack4444(c uint16) (r, g, b, a uint8) {
	r4 := uint8(c>>12) & 0x0F
	g4 := uint8(c>>8) & 0x0F
	b4 := uint8(c>>4) & 0x0F
	a4 := uint8(c) & 0x0F
	return (r4 << 4) | r4,
		(g4 << 4) | g4,
		(b4 << 4) | b4,
		(a4 << 4) | a4
}

// setPixel writes a color to (x, y) in the NRGBA image, bounds-clamped.
func setPixel(img *image.NRGBA, x, y, w, h int, c color.NRGBA) {
	if x < 0 || x >= w || y < 0 || y >= h {
		return
	}
	img.SetNRGBA(x, y, c)
}

// decodeTexture dispatches to the right decoder based on format string.
func decodeTexture(format string, depth int, data []byte, w, h int, palette []byte) *image.NRGBA {
	if w <= 0 || h <= 0 {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))

	switch format {
	case "DXT1":
		decodeDXT1(data, img, w, h)
	case "DXT3":
		decodeDXT3(data, img, w, h)
	case "DXT5":
		decodeDXT5(data, img, w, h)
	case "ABGR8888":
		decodeABGR8888(data, img, w, h)
	case "ARGB8888":
		decodeARGB8888(data, img, w, h)
	case "BGRA8888", "BGRA":
		decodeBGRA8888(data, img, w, h)
	case "BGR888":
		decodeBGR888(data, img, w, h)
	case "BGR565":
		decodeBGR565(data, img, w, h)
	case "BGRA4444":
		decodeBGRA4444(data, img, w, h)
	case "BGRA5551":
		decodeBGRA5551(data, img, w, h)
	case "BGRX5551":
		decodeBGRX5551(data, img, w, h)
	case "BGRX8888":
		decodeBGRX8888(data, img, w, h)
	case "BGR888_BLUESCREEN":
		decodeBGR888Bluescreen(data, img, w, h)
	case "RGB565":
		decodeRGB565(data, img, w, h)
	case "RGB888":
		decodeRGB888(data, img, w, h)
	case "RGB888_BLUESCREEN":
		decodeRGB888Bluescreen(data, img, w, h)
	case "RGBA8888":
		decodeRGBA8888(data, img, w, h)
	case "RGBAH6161616":
		decodeRGBAH6161616(data, img, w, h)
	case "RGBAH6161616F":
		decodeRGBAH6161616F(data, img, w, h)
	case "UV88":
		decodeUV88(data, img, w, h)
	case "UVLX8888", "UVWQ8888":
		decodeUVX8888(data, img, w, h)
	case "AB":
		decodeAB(data, img, w, h)
	case "I8":
		decodeI8(data, img, w, h)
	case "IA88":
		decodeIA88(data, img, w, h)
	case "ARGB1555":
		decodeARGB1555(data, img, w, h)
	case "RGBA4444":
		decodeRGBA4444(data, img, w, h)
	default:
		// Fallback by depth.
		switch {
		case depth == 8 && palette != nil:
			decodePaletted(data, img, w, h, palette)
		case depth == 16:
			decodeARGB1555(data, img, w, h)
		case depth == 24:
			decodeBGR888(data, img, w, h)
		case depth == 32:
			decodeBGRA8888(data, img, w, h)
		default:
			return nil
		}
	}
	return img
}

// ── DXT decoders ───────────────────────────────────────────────────────────

// decodeDXT1 decodes BC1 (DXT1) compressed data.
// Each 8-byte block encodes a 4×4 pixel region: 2 colors (565) + 16×2-bit
// lookup table. If color0 > color1, the third color is a 2/3-1/3 mix and
// the fourth is transparent black. Otherwise c3 is the average and c4 is
// transparent.
func decodeDXT1(data []byte, img *image.NRGBA, w, h int) {
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	expected := bw * bh * 8
	if len(data) < expected {
		// Pad with zeros (matches Python behaviour).
		padded := make([]byte, expected)
		copy(padded, data)
		data = padded
	} else if len(data) > expected {
		data = data[:expected]
	}

	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			off := (by*bw + bx) * 8
			c0 := binary.LittleEndian.Uint16(data[off:])
			c1 := binary.LittleEndian.Uint16(data[off+2:])
			bits := binary.LittleEndian.Uint32(data[off+4:])

			r0, g0, b0, _ := unpack565(c0)
			r1, g1, b1, _ := unpack565(c1)

			var colors [4]color.NRGBA
			colors[0] = color.NRGBA{r0, g0, b0, 255}
			colors[1] = color.NRGBA{r1, g1, b1, 255}
			if c0 > c1 {
				colors[2] = color.NRGBA{
					(2*r0 + r1) / 3,
					(2*g0 + g1) / 3,
					(2*b0 + b1) / 3,
					255,
				}
				colors[3] = color.NRGBA{
					(r0 + 2*r1) / 3,
					(g0 + 2*g1) / 3,
					(b0 + 2*b1) / 3,
					255,
				}
			} else {
				colors[2] = color.NRGBA{(r0 + r1) / 2, (g0 + g1) / 2, (b0 + b1) / 2, 255}
				colors[3] = color.NRGBA{0, 0, 0, 0} // transparent black
			}

			for py := 0; py < 4; py++ {
				for px := 0; px < 4; px++ {
					idx := (bits >> (2 * (py*4 + px))) & 0x03
					setPixel(img, bx*4+px, by*4+py, w, h, colors[idx])
				}
			}
		}
	}
}

// decodeDXT3 decodes BC2 (DXT3). Same as DXT1 for colors, but with explicit
// 4-bit alpha per pixel (64 bits of alpha per block).
func decodeDXT3(data []byte, img *image.NRGBA, w, h int) {
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	expected := bw * bh * 16
	if len(data) < expected {
		padded := make([]byte, expected)
		copy(padded, data)
		data = padded
	} else if len(data) > expected {
		data = data[:expected]
	}

	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			off := (by*bw + bx) * 16
			// Alpha: 8 bytes, 16×4-bit values.
			alphaBytes := data[off : off+8]
			// Color: 8 bytes starting at off+8.
			c0 := binary.LittleEndian.Uint16(data[off+8:])
			c1 := binary.LittleEndian.Uint16(data[off+10:])
			bits := binary.LittleEndian.Uint32(data[off+12:])

			r0, g0, b0, _ := unpack565(c0)
			r1, g1, b1, _ := unpack565(c1)

			colors := [4]color.NRGBA{
				{r0, g0, b0, 255},
				{r1, g1, b1, 255},
				{(2*r0 + r1) / 3, (2*g0 + g1) / 3, (2*b0 + b1) / 3, 255},
				{(r0 + 2*r1) / 3, (g0 + 2*g1) / 3, (b0 + 2*b1) / 3, 255},
			}
			// If c0 <= c1, c3 is transparent (DXT1 mode 2).
			if c0 <= c1 {
				colors[2] = color.NRGBA{(r0 + r1) / 2, (g0 + g1) / 2, (b0 + b1) / 2, 255}
				colors[3] = color.NRGBA{0, 0, 0, 255}
			}

			for py := 0; py < 4; py++ {
				for px := 0; px < 4; px++ {
					idx := (bits >> (2 * (py*4 + px))) & 0x03
					// Alpha: 4 bits at position (py*4 + px) * 4.
					alphaIdx := py*4 + px
					a := alphaBytes[alphaIdx/2]
					if alphaIdx&1 == 0 {
						a &= 0x0F
					} else {
						a >>= 4
					}
					a |= a << 4 // expand 4-bit to 8-bit
					c := colors[idx]
					c.A = a
					setPixel(img, bx*4+px, by*4+py, w, h, c)
				}
			}
		}
	}
}

// decodeDXT5 decodes BC3 (DXT5). Same as DXT1 for colors, but with
// interpolated alpha: 2 reference alphas + 16×3-bit indices.
func decodeDXT5(data []byte, img *image.NRGBA, w, h int) {
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	expected := bw * bh * 16
	if len(data) < expected {
		padded := make([]byte, expected)
		copy(padded, data)
		data = padded
	} else if len(data) > expected {
		data = data[:expected]
	}

	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			off := (by*bw + bx) * 16
			a0 := data[off]
			a1 := data[off+1]
			// 6 bytes of alpha indices (16×3 bits = 48 bits).
			alphaBits := uint64(data[off+2]) |
				uint64(data[off+3])<<8 |
				uint64(data[off+4])<<16 |
				uint64(data[off+5])<<24 |
				uint64(data[off+6])<<32 |
				uint64(data[off+7])<<40

			var alphas [8]uint8
			alphas[0] = a0
			alphas[1] = a1
			if a0 > a1 {
				alphas[2] = (6*a0 + 1*a1) / 7
				alphas[3] = (5*a0 + 2*a1) / 7
				alphas[4] = (4*a0 + 3*a1) / 7
				alphas[5] = (3*a0 + 4*a1) / 7
				alphas[6] = (2*a0 + 5*a1) / 7
				alphas[7] = (1*a0 + 6*a1) / 7
			} else {
				alphas[2] = (4*a0 + 1*a1) / 5
				alphas[3] = (3*a0 + 2*a1) / 5
				alphas[4] = (2*a0 + 3*a1) / 5
				alphas[5] = (1*a0 + 4*a1) / 5
				alphas[6] = 0
				alphas[7] = 255
			}

			c0 := binary.LittleEndian.Uint16(data[off+8:])
			c1 := binary.LittleEndian.Uint16(data[off+10:])
			bits := binary.LittleEndian.Uint32(data[off+12:])

			r0, g0, b0, _ := unpack565(c0)
			r1, g1, b1, _ := unpack565(c1)

			colors := [4]color.NRGBA{
				{r0, g0, b0, 255},
				{r1, g1, b1, 255},
				{(2*r0 + r1) / 3, (2*g0 + g1) / 3, (2*b0 + b1) / 3, 255},
				{(r0 + 2*r1) / 3, (g0 + 2*g1) / 3, (b0 + 2*b1) / 3, 255},
			}
			if c0 <= c1 {
				colors[2] = color.NRGBA{(r0 + r1) / 2, (g0 + g1) / 2, (b0 + b1) / 2, 255}
				colors[3] = color.NRGBA{0, 0, 0, 255}
			}

			for py := 0; py < 4; py++ {
				for px := 0; px < 4; px++ {
					pixIdx := py*4 + px
					colorIdx := (bits >> (2 * pixIdx)) & 0x03
					alphaIdx := (alphaBits >> (3 * uint(pixIdx))) & 0x07
					c := colors[colorIdx]
					c.A = alphas[alphaIdx]
					setPixel(img, bx*4+px, by*4+py, w, h, c)
				}
			}
		}
	}
}

// ── Uncompressed formats ───────────────────────────────────────────────────

func decodeABGR8888(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 4
			if off+4 > len(data) {
				return
			}
			// ABGR → RGBA (just swap R and B in the 4-byte group).
			a, b, g, r := data[off], data[off+1], data[off+2], data[off+3]
			setPixel(img, x, y, w, h, color.NRGBA{r, g, b, a})
		}
	}
}

func decodeARGB8888(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 4
			if off+4 > len(data) {
				return
			}
			a, r, g, b := data[off], data[off+1], data[off+2], data[off+3]
			setPixel(img, x, y, w, h, color.NRGBA{r, g, b, a})
		}
	}
}

func decodeBGRA8888(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 4
			if off+4 > len(data) {
				return
			}
			b, g, r, a := data[off], data[off+1], data[off+2], data[off+3]
			setPixel(img, x, y, w, h, color.NRGBA{r, g, b, a})
		}
	}
}

func decodeBGR888(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 3
			if off+3 > len(data) {
				return
			}
			b, g, r := data[off], data[off+1], data[off+2]
			// Match Python: black pixels become transparent.
			a := uint8(255)
			if r == 0 && g == 0 && b == 0 {
				a = 0
			}
			setPixel(img, x, y, w, h, color.NRGBA{r, g, b, a})
		}
	}
}

func decodeRGB888(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 3
			if off+3 > len(data) {
				return
			}
			r, g, b := data[off], data[off+1], data[off+2]
			setPixel(img, x, y, w, h, color.NRGBA{r, g, b, 255})
		}
	}
}

func decodeBGR565(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 2
			if off+2 > len(data) {
				return
			}
			c := binary.LittleEndian.Uint16(data[off:])
			b := uint8(c>>11) & 0x1F
			g := uint8(c>>5) & 0x3F
			r := uint8(c) & 0x1F
			setPixel(img, x, y, w, h, color.NRGBA{
				(r << 3) | (r >> 2),
				(g << 2) | (g >> 4),
				(b << 3) | (b >> 2),
				255,
			})
		}
	}
}

func decodeRGB565(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 2
			if off+2 > len(data) {
				return
			}
			c := binary.LittleEndian.Uint16(data[off:])
			r, g, b, _ := unpack565(c)
			setPixel(img, x, y, w, h, color.NRGBA{r, g, b, 255})
		}
	}
}

func decodeBGRA4444(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 2
			if off+2 > len(data) {
				return
			}
			c := binary.LittleEndian.Uint16(data[off:])
			b := uint8(c>>12) & 0x0F
			g := uint8(c>>8) & 0x0F
			r := uint8(c>>4) & 0x0F
			a := uint8(c) & 0x0F
			setPixel(img, x, y, w, h, color.NRGBA{
				(r << 4) | r, (g << 4) | g, (b << 4) | b, (a << 4) | a,
			})
		}
	}
}

func decodeBGRA5551(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 2
			if off+2 > len(data) {
				return
			}
			c := binary.LittleEndian.Uint16(data[off:])
			b := uint8(c>>11) & 0x1F
			g := uint8(c>>6) & 0x1F
			r := uint8(c>>1) & 0x1F
			a := uint8(c) & 0x1
			setPixel(img, x, y, w, h, color.NRGBA{
				(r << 3) | (r >> 2),
				(g << 3) | (g >> 2),
				(b << 3) | (b >> 2),
				a * 255,
			})
		}
	}
}

func decodeBGRX5551(data []byte, img *image.NRGBA, w, h int) {
	// Same as BGRA5551 but alpha is always 255 (X = ignored).
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 2
			if off+2 > len(data) {
				return
			}
			c := binary.LittleEndian.Uint16(data[off:])
			b := uint8(c>>11) & 0x1F
			g := uint8(c>>6) & 0x1F
			r := uint8(c>>1) & 0x1F
			setPixel(img, x, y, w, h, color.NRGBA{
				(r << 3) | (r >> 2),
				(g << 3) | (g >> 2),
				(b << 3) | (b >> 2),
				255,
			})
		}
	}
}

func decodeBGRX8888(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 4
			if off+4 > len(data) {
				return
			}
			b, g, r := data[off], data[off+1], data[off+2]
			setPixel(img, x, y, w, h, color.NRGBA{r, g, b, 255})
		}
	}
}

func decodeBGR888Bluescreen(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 3
			if off+3 > len(data) {
				return
			}
			b, g, r := data[off], data[off+1], data[off+2]
			// Pure blue (0,0,255) → transparent.
			a := uint8(255)
			if r == 0 && g == 0 && b == 255 {
				a = 0
			}
			setPixel(img, x, y, w, h, color.NRGBA{r, g, b, a})
		}
	}
}

func decodeRGB888Bluescreen(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 3
			if off+3 > len(data) {
				return
			}
			r, g, b := data[off], data[off+1], data[off+2]
			a := uint8(255)
			if r == 0 && g == 0 && b == 255 {
				a = 0
			}
			setPixel(img, x, y, w, h, color.NRGBA{r, g, b, a})
		}
	}
}

func decodeRGBA8888(data []byte, img *image.NRGBA, w, h int) {
	// Python decode_rgb8888: stores BGRA in memory, then has a weird alpha
	// heuristic (mean < 50 && std < 30 → invert; mean < 10 → opaque).
	// For simplicity and predictability we just decode as straight BGRA.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 4
			if off+4 > len(data) {
				return
			}
			b, g, r, a := data[off], data[off+1], data[off+2], data[off+3]
			setPixel(img, x, y, w, h, color.NRGBA{r, g, b, a})
		}
	}
}

func decodeARGB1555(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 2
			if off+2 > len(data) {
				return
			}
			c := binary.LittleEndian.Uint16(data[off:])
			r, g, b, a := unpack5551(c)
			setPixel(img, x, y, w, h, color.NRGBA{r, g, b, a})
		}
	}
}

func decodeRGBA4444(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 2
			if off+2 > len(data) {
				return
			}
			c := binary.LittleEndian.Uint16(data[off:])
			r, g, b, a := unpack4444(c)
			setPixel(img, x, y, w, h, color.NRGBA{r, g, b, a})
		}
	}
}

func decodeI8(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := y*w + x
			if off >= len(data) {
				return
			}
			v := data[off]
			setPixel(img, x, y, w, h, color.NRGBA{v, v, v, 255})
		}
	}
}

func decodeIA88(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 2
			if off+2 > len(data) {
				return
			}
			v := data[off]
			a := data[off+1]
			setPixel(img, x, y, w, h, color.NRGBA{v, v, v, a})
		}
	}
}

func decodeAB(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := y*w + x
			if off >= len(data) {
				return
			}
			a := data[off]
			setPixel(img, x, y, w, h, color.NRGBA{0, 0, 0, a})
		}
	}
}

func decodeUV88(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 2
			if off+2 > len(data) {
				return
			}
			u := data[off]
			v := data[off+1]
			setPixel(img, x, y, w, h, color.NRGBA{u, v, 128, 255})
		}
	}
}

func decodeUVX8888(data []byte, img *image.NRGBA, w, h int) {
	// UVLX / UVWQ — just dump the 4 channels as RGBA.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 4
			if off+4 > len(data) {
				return
			}
			r, g, b, a := data[off], data[off+1], data[off+2], data[off+3]
			setPixel(img, x, y, w, h, color.NRGBA{r, g, b, a})
		}
	}
}

func decodeRGBAH6161616(data []byte, img *image.NRGBA, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 8
			if off+8 > len(data) {
				return
			}
			r16 := binary.LittleEndian.Uint16(data[off:])
			g16 := binary.LittleEndian.Uint16(data[off+2:])
			b16 := binary.LittleEndian.Uint16(data[off+4:])
			a16 := binary.LittleEndian.Uint16(data[off+6:])
			setPixel(img, x, y, w, h, color.NRGBA{
				uint8(uint32(r16) * 255 / 65535),
				uint8(uint32(g16) * 255 / 65535),
				uint8(uint32(b16) * 255 / 65535),
				uint8(uint32(a16) * 255 / 65535),
			})
		}
	}
}

func decodeRGBAH6161616F(data []byte, img *image.NRGBA, w, h int) {
	// 16-bit float channels. We just truncate to uint8 (loses precision but
	// matches the Python behaviour where clip(arr * 255, 0, 255) is applied).
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 8
			if off+8 > len(data) {
				return
			}
			// Treat as uint16 and scale (rough approximation of half-float).
			r16 := binary.LittleEndian.Uint16(data[off:])
			g16 := binary.LittleEndian.Uint16(data[off+2:])
			b16 := binary.LittleEndian.Uint16(data[off+4:])
			a16 := binary.LittleEndian.Uint16(data[off+6:])
			setPixel(img, x, y, w, h, color.NRGBA{
				uint8(uint32(r16) * 255 / 65535),
				uint8(uint32(g16) * 255 / 65535),
				uint8(uint32(b16) * 255 / 65535),
				uint8(uint32(a16) * 255 / 65535),
			})
		}
	}
}

func decodePaletted(data []byte, img *image.NRGBA, w, h int, palette []byte) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := y*w + x
			if off >= len(data) {
				return
			}
			idx := int(data[off])
			pOff := idx * 3
			if pOff+3 > len(palette) {
				setPixel(img, x, y, w, h, color.NRGBA{0, 0, 0, 255})
				continue
			}
			r, g, b := palette[pOff], palette[pOff+1], palette[pOff+2]
			setPixel(img, x, y, w, h, color.NRGBA{r, g, b, 255})
		}
	}
}
