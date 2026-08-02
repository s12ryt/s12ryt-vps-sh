package qrcode

import (
	"bytes"
	"image/png"
	"testing"
)

func TestPNGRendererProducesReadableSquareQRCode(t *testing.T) {
	renderer, err := NewPNGRenderer()
	if err != nil {
		t.Fatalf("NewPNGRenderer() error = %v", err)
	}
	payload := "vless://550e8400-e29b-41d4-a716-446655440000@[2001:db8::10]:24443"
	data, err := renderer.RenderPNG(payload)
	if err != nil {
		t.Fatalf("RenderPNG() error = %v", err)
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("RenderPNG() did not return PNG data: %x", data)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode QR PNG: %v", err)
	}
	if config.Width != config.Height || config.Width < 128 {
		t.Fatalf("QR dimensions = %dx%d", config.Width, config.Height)
	}
}

func TestPNGRendererRejectsEmptyPayload(t *testing.T) {
	renderer, err := NewPNGRenderer()
	if err != nil {
		t.Fatalf("NewPNGRenderer() error = %v", err)
	}
	if _, err := renderer.RenderPNG(""); err == nil {
		t.Fatal("RenderPNG() accepted an empty payload")
	}
}
