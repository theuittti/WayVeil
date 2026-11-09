package gui

import (
	"bytes"
	"encoding/base64"
	"image/png"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// qrDataURL renders s as a 256 px QR code PNG data URL.
func qrDataURL(s string) (string, error) {
	bm, err := qrcode.NewQRCodeWriter().Encode(s, gozxing.BarcodeFormat_QR_CODE, 256, 256, nil)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, bm); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
