// Package btx — helpers.go — exported helpers for handlers.
package btx

import (
	"image"
)

// DecodeImageForHandler decodes any image format into NRGBA.
// Used by /img2webp handler.
func DecodeImageForHandler(data []byte) (*image.NRGBA, error) {
	img, _, err := decodeImage(data)
	return img, err
}

// EncodeWebPForHandler encodes an image as WebP (via cwebp binary or JPEG fallback).
// Used by /img2webp handler.
func EncodeWebPForHandler(img image.Image, quality int) ([]byte, error) {
	return encodeWebP(img, quality)
}

// EncodeJPEGForHandler encodes an image as JPEG.
func EncodeJPEGForHandler(img image.Image, quality int) ([]byte, error) {
	return encodeJPEG(img, quality)
}
