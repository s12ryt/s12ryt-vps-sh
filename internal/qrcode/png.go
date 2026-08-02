package qrcode

import (
	"errors"
	"fmt"

	go_qr "github.com/piglig/go-qr"
)

const (
	qrScale  = 8
	qrBorder = 4
)

type PNGRenderer struct{}

func NewPNGRenderer() (*PNGRenderer, error) {
	return &PNGRenderer{}, nil
}

func (*PNGRenderer) RenderPNG(payload string) ([]byte, error) {
	if payload == "" {
		return nil, errors.New("QR payload is empty")
	}
	code, err := go_qr.EncodeText(payload, go_qr.Medium)
	if err != nil {
		return nil, fmt.Errorf("encode QR payload: %w", err)
	}
	data, err := code.ToPNGBytes(go_qr.NewQrCodeImgConfig(qrScale, qrBorder))
	if err != nil {
		return nil, fmt.Errorf("render QR PNG: %w", err)
	}
	return data, nil
}
