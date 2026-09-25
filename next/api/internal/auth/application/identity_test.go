package application

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestValidateSystemIconRejectsTruncatedImageWithValidHeader(t *testing.T) {
	var encoded bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	full := encoded.Bytes()
	if len(full) < 16 {
		t.Fatalf("unexpectedly short png: %d bytes", len(full))
	}
	truncated := full[:len(full)-8]
	if _, format, err := image.DecodeConfig(bytes.NewReader(truncated)); err != nil || format != "png" {
		t.Fatalf("fixture must have a valid png header: format=%q err=%v", format, err)
	}
	if _, err := validateSystemIcon(truncated, "image/png"); err == nil {
		t.Fatal("truncated png was accepted")
	}
}

func TestValidateSystemIconRejectsUndecodablePayload(t *testing.T) {
	data := []byte("not an image")
	if _, err := validateSystemIcon(data, "image/png"); err == nil {
		t.Fatal("invalid image was accepted")
	}
}
