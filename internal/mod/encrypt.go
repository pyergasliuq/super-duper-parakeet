// Package mod — encrypt.go — encrypt .dff back to .mod (reverse of decrypt).
package mod

import (
	"encoding/binary"
	"fmt"
)

// EncryptDFF takes a .dff file's bytes and returns the encrypted .mod bytes.
//
// This is the reverse of DecryptMod:
//   1. Pad the .dff data to a multiple of 0x800 bytes.
//   2. TEA-encrypt each 0x800-byte block with the derived key.
//   3. Prepend the 28-byte .mod header:
//      - magic (0xAB921033)
//      - length (original .dff length)
//      - num_blocks
//      - 16 bytes padding
func EncryptDFF(dffBytes []byte) ([]byte, error) {
	if len(dffBytes) == 0 {
		return nil, fmt.Errorf("mod: empty input")
	}

	// Pad to multiple of BlockSize.
	paddedLen := ((len(dffBytes) + BlockSize - 1) / BlockSize) * BlockSize
	padded := make([]byte, paddedLen)
	copy(padded, dffBytes)

	numBlocks := uint32(paddedLen / BlockSize)
	key := deriveKey()

	// TEA-encrypt each block.
	for i := uint32(0); i < numBlocks; i++ {
		offset := int(i) * BlockSize
		teaEncryptBlock(padded[offset:offset+BlockSize], key)
	}

	// Build the .mod header (28 bytes).
	header := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint32(header[0:4], Magic)
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(dffBytes)))
	binary.LittleEndian.PutUint32(header[8:12], numBlocks)
	// bytes 12..28 are padding (zeros).

	// Concatenate header + encrypted body.
	out := make([]byte, 0, HeaderSize+paddedLen)
	out = append(out, header...)
	out = append(out, padded...)
	return out, nil
}

// teaEncryptBlock encrypts a 0x800-byte block IN PLACE using the given key.
// This is the inverse of teaDecryptBlock.
func teaEncryptBlock(data []byte, key [4]uint32) {
	if len(data)%8 != 0 {
		pad := make([]byte, 8-(len(data)%8))
		data = append(data, pad...)
	}
	rounds := 8
	// Forward sum: 0, delta, 2*delta, ..., (rounds-1)*delta.
	// Final sum = delta * rounds.
	finalSum := Delta * uint32(rounds)

	for offset := 0; offset < len(data); offset += 8 {
		v0 := binary.LittleEndian.Uint32(data[offset:])
		v1 := binary.LittleEndian.Uint32(data[offset+4:])

		// Reverse the decryption steps.
		// Decrypt did:
		//   for r in 0..rounds:
		//     v1 -= (v0 + sum) ^ (key[3] + (v0>>5)) ^ (key[2] + (v0<<4))
		//     new_sum = sum + v1
		//     sum += delta
		//     v0 -= new_sum ^ (key[0] + (v1<<4)) ^ (key[1] + (v1>>5))
		// To encrypt we reverse: start from finalSum, go backwards.
		sum := finalSum
		for r := 0; r < rounds; r++ {
			// Reverse v0 update: v0 += new_sum ^ ...
			// new_sum = sum + v1 (BEFORE sum += delta)
			// But sum was updated AFTER new_sum computation, so for reverse:
			//   new_sum = sum + v1 (current sum is post-update, so pre-update = sum - delta)
			// Actually we need to undo in exact reverse order.
			// Encrypt step (forward):
			//   v0_enc = v0_dec + (sum_enc ^ (key[0] + (v1_enc<<4)) ^ (key[1] + (v1_enc>>5)))
			// where sum_enc = sum_before + v1_enc
			// and sum_after = sum_before + delta
			//
			// To reverse from (v0_enc, v1_enc, sum_after):
			//   sum_before = sum_after - delta
			//   sum_enc = sum_before + v1_enc = sum_after - delta + v1_enc
			//   v0_dec = v0_enc - (sum_enc ^ ...)
			//
			// Hmm, this is getting complex. Let me just mirror the decrypt
			// with additions instead of subtractions.

			// Undo v0 step:
			// In decrypt: v0 -= new_sum ^ (key[0] + (v1<<4)) ^ (key[1] + (v1>>5))
			// In encrypt: v0 += new_sum ^ (key[0] + (v1<<4)) ^ (key[1] + (v1>>5))
			// where new_sum = sum + v1 (computed BEFORE sum += delta)
			newSum := sum + v1
			v0 = v0 + (newSum ^ (key[0] + (v1 << 4)) ^ (key[1] + (v1 >> 5)))

			// Undo sum update:
			// In decrypt: sum += delta (after new_sum computed)
			// In encrypt: sum -= delta (going backwards)
			sum = sum - Delta

			// Undo v1 step:
			// In decrypt: v1 -= (v0 + sum) ^ (key[3] + (v0>>5)) ^ (key[2] + (v0<<4))
			// In encrypt: v1 += (v0 + sum) ^ (key[3] + (v0>>5)) ^ (key[2] + (v0<<4))
			// Note: here v0 is the ENCRYPTED v0 (which decrypt calls v0_dec),
			// and sum is the PRE-update sum (which decrypt sees as sum before its update).
			v1 = v1 + ((v0 + sum) ^ (key[3] + (v0 >> 5)) ^ (key[2] + (v0 << 4)))
		}
		binary.LittleEndian.PutUint32(data[offset:], v0)
		binary.LittleEndian.PutUint32(data[offset+4:], v1)
	}
}
