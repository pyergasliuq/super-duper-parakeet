// Package txd — info.go — extract texture metadata without decoding.
package txd

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// TextureInfo is one texture's metadata (name, format, dimensions).
type TextureInfo struct {
	Name   string
	Format string
	Width  int
	Height int
	Depth  int
}

// ParseInfo extracts metadata for all textures in a .txd without decoding
// the pixel data. Much faster than Parse for /txd_info.
func ParseInfo(data []byte) ([]TextureInfo, error) {
	p := &infoParser{data: data}
	return p.parse()
}

type infoParser struct {
	data []byte
	pos  int
}

func (p *infoParser) parse() ([]TextureInfo, error) {
	var out []TextureInfo
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
			// skip
		case 1:
			if !insideTexture {
				if p.pos+4 > len(p.data) {
					break
				}
				p.pos += 4 // num_textures + padding
			} else {
				info, err := p.parseNativeTextureInfo()
				if err != nil {
					return out, err
				}
				if info != nil {
					out = append(out, *info)
				}
				insideTexture = false
			}
		case 21:
			insideTexture = true
		case 3:
			// skip
		default:
			remaining := int(chunkSize) - 4
			if remaining < 0 {
				remaining = 0
			}
			if p.pos+remaining > len(p.data) {
				break
			}
			p.pos += remaining
		}
	}
	return out, nil
}

// parseNativeTextureInfo extracts name/format/dimensions without decoding.
func (p *infoParser) parseNativeTextureInfo() (*TextureInfo, error) {
	if p.pos+80 > len(p.data) {
		return nil, fmt.Errorf("not enough data for texture header")
	}
	p.pos += 4 // version
	p.pos += 4 // filter_flags

	nameBytes := p.data[p.pos : p.pos+32]
	p.pos += 32
	name := strings.TrimRight(string(nameBytes), "\x00")

	p.pos += 32 // alpha_name
	p.pos += 4  // alpha_flags

	formatBytes := p.data[p.pos : p.pos+4]
	p.pos += 4
	format := strings.TrimRight(string(formatBytes), "\x00")

	width := int(binary.LittleEndian.Uint16(p.data[p.pos:]))
	p.pos += 2
	height := int(binary.LittleEndian.Uint16(p.data[p.pos:]))
	p.pos += 2
	depth := int(p.data[p.pos])
	p.pos++
	p.pos++ // mipmap_count
	p.pos++ // texcode_type
	p.pos++ // flags

	// Skip palette + data + mipmaps — we don't need them for info.
	// (Caller will not use the rest of this chunk.)
	return &TextureInfo{
		Name:   name,
		Format: format,
		Width:  width,
		Height: height,
		Depth:  depth,
	}, nil
}

func (p *infoParser) readU32() (uint32, error) {
	if p.pos+4 > len(p.data) {
		return 0, fmt.Errorf("EOF")
	}
	v := binary.LittleEndian.Uint32(p.data[p.pos:])
	p.pos += 4
	return v, nil
}
