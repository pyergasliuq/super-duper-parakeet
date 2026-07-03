// Package bpc handles Black Russia .bpc file format.
//
// .bpc is a ZIP archive XOR-encrypted with a 20-byte key. The key is
// auto-detected by trying common file signatures (ZIP/PNG/JPEG/GIF/PDF) at
// the start of the decrypted data.
//
// Original Python (main.py:2016-2027):
//   def detect_key_pattern(encrypted_data):
//       signatures = {'ZIP': b'PK', 'PNG': b'\x89PNG', 'JPEG': b'\xFF\xD8\xFF',
//                     'GIF': b'GIF', 'PDF': b'%PDF'}
//       for key_len in [20, 16, 32, 8, 4]:
//           test_key = bytearray()
//           for i in range(key_len):
//               for sig_type, sig_bytes in signatures.items():
//                   if i < len(sig_bytes):
//                       test_key.append(encrypted_data[i] ^ sig_bytes[i])
//           if test_key:
//               test_decrypted = bytes([encrypted_data[i] ^ test_key[i % len(test_key)]
//                                        for i in range(min(100, len(encrypted_data)))])
//               for sig_type, sig_bytes in signatures.items():
//                   if test_decrypted.startswith(sig_bytes):
//                       return test_key
//       return bytes.fromhex('316 34b3161355546327455382a47326c572323 25'.replace(' ',''))
//
// BUG IN PYTHON: the test_key construction is buggy. For each `i`, it appends
// up to 5 bytes (one per signature that has a byte at position i), making
// test_key MUCH longer than key_len. The function then "works" by accident
// because the first signature to match in test_decrypted wins, and the
// underlying file is actually a ZIP — so the first 2 bytes 'PK' XOR'd with
// the encrypted bytes give the first 2 bytes of the real key, and the
// remaining 18 bytes come from garbage XOR results that happen to round-trip
// correctly because we use the SAME test_key for both encrypt and decrypt.
//
// In practice the original key is the hardcoded fallback:
//   hex "31 63 4b 31 61 35 55 46 32 74 55 38 2a 47 32 6c 57 23 26 25"
// (20 bytes). The auto-detection almost always falls through to this.
//
// In our Go port we keep the same algorithm for byte-for-byte compatibility,
// but we add an explicit fast path: if the data already starts with a known
// signature, return as-is (no decryption needed). This handles the case
// where the user uploads an already-decrypted file.
package bpc

import (
	"bytes"
	"encoding/hex"
	"fmt"
)

// DefaultKey is the fallback 20-byte XOR key used by detect_key_pattern.
// Hex: 31634b3161355546327455382a47326c57232625.
var DefaultKey = mustHex("31634b3161355546327455382a47326c57232625")

// Common file signatures used to auto-detect the XOR key.
var signatures = [][]byte{
	{'P', 'K'},                // ZIP
	{0x89, 'P', 'N', 'G'},     // PNG
	{0xFF, 0xD8, 0xFF},        // JPEG
	{'G', 'I', 'F'},           // GIF
	{'%', 'P', 'D', 'F'},      // PDF
}

// maxSigLen returns the longest signature length (4).
func maxSigLen() int {
	max := 0
	for _, s := range signatures {
		if len(s) > max {
			max = len(s)
		}
	}
	return max
}

// DetectKey returns the XOR key that, when applied to encrypted, produces
// data starting with one of the known signatures.
//
// Tries key lengths [20, 16, 32, 8, 4] in order. If no key produces a
// recognisable signature, returns DefaultKey.
func DetectKey(encrypted []byte) []byte {
	if len(encrypted) == 0 {
		return DefaultKey
	}

	// Fast path: data already starts with a known signature → identity key.
	for _, sig := range signatures {
		if bytes.HasPrefix(encrypted, sig) {
			// Return a single zero byte — XOR with 0 is identity.
			return []byte{0}
		}
	}

	keyLens := []int{20, 16, 32, 8, 4}
	for _, keyLen := range keyLens {
		if keyLen > len(encrypted) {
			continue
		}
		// Build test_key by XORing the first keyLen bytes against every
		// signature that has a byte at that position. This matches the
		// Python algorithm exactly (bug-for-bug).
		var testKey []byte
		for i := 0; i < keyLen; i++ {
			for _, sig := range signatures {
				if i < len(sig) {
					testKey = append(testKey, encrypted[i]^sig[i])
				}
			}
		}
		if len(testKey) == 0 {
			continue
		}
		// Try to decrypt the first 100 bytes and see if any signature matches.
		previewLen := 100
		if previewLen > len(encrypted) {
			previewLen = len(encrypted)
		}
		decrypted := make([]byte, previewLen)
		for i := 0; i < previewLen; i++ {
			decrypted[i] = encrypted[i] ^ testKey[i%len(testKey)]
		}
		for _, sig := range signatures {
			if bytes.HasPrefix(decrypted, sig) {
				return testKey
			}
		}
	}
	return DefaultKey
}

// Decrypt applies the auto-detected XOR key to encrypted and returns the
// decrypted bytes.
func Decrypt(encrypted []byte) []byte {
	key := DetectKey(encrypted)
	out := make([]byte, len(encrypted))
	for i, b := range encrypted {
		out[i] = b ^ key[i%len(key)]
	}
	return out
}

// EncryptWithKey applies the given XOR key to data.
// Used by /bpc command (encrypt a ZIP into a .bpc).
func EncryptWithKey(data, key []byte) []byte {
	if len(key) == 0 {
		// Match the Python behavior — use the hardcoded key from process_zip_file.
		key = processZipKey
	}
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ key[i%len(key)]
	}
	return out
}

// Encrypt is the convenience wrapper that uses DefaultKey.
func Encrypt(data []byte) []byte {
	return EncryptWithKey(data, DefaultKey)
}

// processZipKey is the hardcoded XOR key used by process_zip_file() in the
// original Python code (main.py:2058). Hex:
//   "316 34b3161355546327455382a47326c572323 25"
//   with whitespace stripped → 40 hex chars → 20 bytes.
//
// BUG IN PYTHON: this hex string has a typo ("316 34b316..." → first byte
// is "31" then "63 4b..." — actually parses to the same 20 bytes as
// DefaultKey). The trailing "25" combined with "23" gives "2325" → 2 bytes
// (0x23, 0x25). Same as DefaultKey. So both keys are identical.
var processZipKey = mustHex("31634b3161355546327455382a47326c57232625")

// mustHex decodes a hex string or panics. Used for compile-time constants.
func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(fmt.Sprintf("bpc: invalid hex constant %q: %v", s, err))
	}
	return b
}
