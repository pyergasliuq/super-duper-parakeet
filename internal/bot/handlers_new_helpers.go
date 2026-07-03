// Package bot — handlers_new_helpers.go — helpers for handlers_new.go.
package bot

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
)

func jsonUnmarshalImpl(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func jsonMarshalImpl(v any) ([]byte, error) {
	return json.Marshal(v)
}

func jsonMarshalIndentImpl(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

func pngEncodeImpl(w *bytes.Buffer, img *image.NRGBA) error {
	return png.Encode(w, img)
}
