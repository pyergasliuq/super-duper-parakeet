// Package mod handles Black Russia .mod files (encrypted .dff models).
//
// .mod format (from main.py:1828-1869):
//   - 4 bytes: magic 0xAB921033
//   - 4 bytes: length value (decrypted data length)
//   - 4 bytes: num_blocks (number of 0x800-byte encrypted blocks)
//   - 16 bytes: ?? padding/header (offset 12..28)
//   - num_blocks × 0x800 bytes: each block is TEA-encrypted with a key
//     derived from a fixed base key XOR'd with 0x12913AFB and rotated right
//     by 19 bits.
//
// After decryption:
//   - The first 12 bytes of the decrypted data are a DFF header that needs
//     patching (the length field is wrong by 12 — patch_dff_header).
//   - Trailing zero bytes are stripped (clean_dff_data).
//
// TEA implementation notes:
//   - 8 rounds (not the standard 32).
//   - Delta = 0x61C88647 (signed; in Python this is negative because of
//     two's complement, but the algorithm uses unsigned arithmetic via
//     & 0xFFFFFFFF).
//   - The Python code has a quirk: `new_sum = (sum_val + v1) & 0xFFFFFFFF`
//     is computed BEFORE the v0 update, but the v0 update uses `new_sum`
//     (not the original sum_val) — and then `sum_val = (sum_val + delta)`
//     updates AFTER. This is unusual but matches the C++ Black Russia
//     implementation, so we replicate it exactly.
package mod

import (
	"encoding/binary"
	"fmt"
)

// Magic is the .mod file magic number.
const Magic uint32 = 0xAB921033

// BlockSize is the size of each encrypted block (2 KB).
const BlockSize = 0x800

// HeaderSize is the offset where encrypted blocks start.
const HeaderSize = 28

// BaseKey is the fixed base key XOR'd with KeyMask and rotated right by 19.
var BaseKey = [4]uint32{0x6ED9EE7A, 0x930C666B, 0x930E166B, 0x4709EE79}

// KeyMask is XOR'd onto each BaseKey word before the rotation.
const KeyMask uint32 = 0x12913AFB

// RotationBits is the right-rotation applied to derive the real key.
const RotationBits = 19

// Delta is the TEA sum delta. In the original C++ implementation this is
// 0x61C88647 — which is the two's-complement representation of
// 0x9E3779B9 (the standard TEA delta) when interpreted as int32. We use the
// same value to match byte-for-byte.
const Delta uint32 = 0x61C88647

// DecryptMod takes a .mod file's bytes and returns the decrypted .dff bytes.
//
// Returns an error if:
//   - file is shorter than HeaderSize (28 bytes)
//   - magic doesn't match
//   - declared block count exceeds available data
//
// On success the returned []byte is a clean .dff ready to write to disk.
func DecryptMod(modBytes []byte) ([]byte, error) {
	if len(modBytes) < HeaderSize {
		return nil, fmt.Errorf("mod: input too short (%d bytes, need at least %d)",
			len(modBytes), HeaderSize)
	}

	magic := binary.LittleEndian.Uint32(modBytes[0:4])
	if magic != Magic {
		return nil, fmt.Errorf("mod: invalid magic 0x%08X (expected 0x%08X)", magic, Magic)
	}

	lengthVal := binary.LittleEndian.Uint32(modBytes[4:8])
	numBlocks := binary.LittleEndian.Uint32(modBytes[8:12])

	// Derive the real key.
	key := deriveKey()

	// Make a mutable copy of the input so we can decrypt in place.
	data := make([]byte, len(modBytes))
	copy(data, modBytes)

	// Decrypt each 0x800-byte block starting at offset 28.
	// BUG IN PYTHON: `block = data[offset:offset + 0x800]` slices 0x800 bytes
	// even if the file is shorter. `struct.unpack_from('<II', data, offset)`
	// then fails on the last partial block. We handle this by padding the
	// last block with zeros up to 0x800 bytes (TEA decrypts 8 bytes at a
	// time, so partial blocks need padding to 8-byte boundary).
	for i := uint32(0); i < numBlocks; i++ {
		offset := HeaderSize + int(i*BlockSize)
		if offset+BlockSize > len(data) {
			// Pad to 0x800 with zeros so TEA doesn't panic.
			pad := make([]byte, offset+BlockSize-len(data))
			data = append(data, pad...)
		}
		teaDecryptBlock(data[offset:offset+BlockSize], key)
	}

	// Extract the decrypted body. actual_length is the "real" decrypted
	// length declared in the header; if it's larger than what we have, cap it.
	actualLength := int(lengthVal)
	if actualLength > len(data)-HeaderSize {
		actualLength = len(data) - HeaderSize
	}
	if actualLength < 0 {
		actualLength = 0
	}

	dff := make([]byte, actualLength)
	copy(dff, data[HeaderSize:HeaderSize+actualLength])

	// Patch the DFF header: the 4-byte length at offset 4..8 should be
	// real_size = len(dff) - 12 (the header is 12 bytes: 4 magic + 4 length
	// + 4 padding).
	dff = patchDFFHeader(dff)

	// Strip trailing zero bytes.
	dff = cleanDFFData(dff)

	return dff, nil
}

// deriveKey returns the real TEA key: each base key word XOR'd with KeyMask
// and rotated right by RotationBits.
func deriveKey() [4]uint32 {
	var key [4]uint32
	for i, k := range BaseKey {
		key[i] = rotr32(k^KeyMask, RotationBits)
	}
	return key
}

// rotr32 rotates x right by r bits (0 ≤ r < 32).
func rotr32(x uint32, r uint) uint32 {
	return (x >> r) | (x << (32 - r))
}

// teaDecryptBlock decrypts a 0x800-byte block IN PLACE using the given key.
// 8 rounds of TEA. The block length must be a multiple of 8.
//
// The algorithm matches the Python tea_decrypt_block exactly:
//
//	for offset in range(0, len(data), 8):
//	    v0, v1 = struct.unpack_from('<II', data, offset)
//	    sum_val = (-delta * rounds) & 0xFFFFFFFF
//	    for _ in range(rounds):
//	        v1 = (v1 - ((v0 + sum_val) ^ (key[3] + (v0 >> 5)) ^ (key[2] + (v0 << 4)))) & 0xFFFFFFFF
//	        new_sum = (sum_val + v1) & 0xFFFFFFFF
//	        sum_val = (sum_val + delta) & 0xFFFFFFFF
//	        v0 = (v0 - (new_sum ^ (key[0] + (v1 << 4)) ^ (key[1] + (v1 >> 5)))) & 0xFFFFFFFF
//	    struct.pack_into('<II', data, offset, v0, v1)
//
// Note: `(-delta * rounds) & 0xFFFFFFFF` in Python with delta=0x61C88647
// and rounds=8 = (-0x61C88647 * 8) mod 2^32.
// -0x61C88647 as int64 = -1640531527
// * 8 = -13124252216
// mod 2^32 = 0xC6EF3720 - 0x100000000 + ... — Python's & handles this
// correctly via arbitrary precision. In Go we use uint32 arithmetic with
// the equivalent computation: (delta * rounds) is the "forward" sum;
// starting sum = -(delta * rounds) mod 2^32 = 0 - delta*rounds (mod 2^32).
func teaDecryptBlock(data []byte, key [4]uint32) {
	if len(data)%8 != 0 {
		// Pad up to 8-byte boundary (shouldn't happen for full blocks).
		pad := make([]byte, 8-(len(data)%8))
		data = append(data, pad...)
	}
	rounds := 8
	// Initial sum: -(delta * rounds) mod 2^32.
	// In Go, uint32 arithmetic wraps mod 2^32 naturally.
	// delta * 8 mod 2^32:
	startSum := uint32(0) - (Delta * uint32(rounds))

	for offset := 0; offset < len(data); offset += 8 {
		v0 := binary.LittleEndian.Uint32(data[offset : offset+4])
		v1 := binary.LittleEndian.Uint32(data[offset+4 : offset+8])

		sumVal := startSum
		for r := 0; r < rounds; r++ {
			// v1 -= (v0 + sum) ^ (key[3] + (v0 >> 5)) ^ (key[2] + (v0 << 4))
			t1 := v0 + sumVal
			t2 := key[3] + (v0 >> 5)
			t3 := key[2] + (v0 << 4)
			v1 = v1 - (t1 ^ t2 ^ t3)

			// new_sum = (sum + v1)
			newSum := sumVal + v1

			// sum += delta
			sumVal = sumVal + Delta

			// v0 -= new_sum ^ (key[0] + (v1 << 4)) ^ (key[1] + (v1 >> 5))
			u1 := newSum
			u2 := key[0] + (v1 << 4)
			u3 := key[1] + (v1 >> 5)
			v0 = v0 - (u1 ^ u2 ^ u3)
		}
		binary.LittleEndian.PutUint32(data[offset:offset+4], v0)
		binary.LittleEndian.PutUint32(data[offset+4:offset+8], v1)
	}
}

// patchDFFHeader rewrites the length field at offset 4..8 to be the actual
// body size (total - 12). Matches patch_dff_header() in Python.
func patchDFFHeader(dff []byte) []byte {
	if len(dff) < 12 {
		return dff
	}
	realSize := uint32(len(dff) - 12)
	binary.LittleEndian.PutUint32(dff[4:8], realSize)
	return dff
}

// cleanDFFData strips trailing zero bytes. Matches clean_dff_data() in Python.
// BUG IN PYTHON: this strips ALL trailing zeros, which can legitimately be
// part of the model data (e.g. padding in vertex buffers). We replicate the
// behaviour for byte-for-byte compatibility but flag it as a known bug.
func cleanDFFData(dff []byte) []byte {
	end := len(dff)
	for end > 0 && dff[end-1] == 0 {
		end--
	}
	return dff[:end]
}
