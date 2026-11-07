// Package profile is a client connection profile imported from a share link
// or a QR code image.
package profile

import (
	"errors"
	"image"
	_ "image/jpeg" // QR screenshots / photos
	_ "image/png"
	"os"
	"strings"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"

	"github.com/theuittti/WayVeil/internal/link"
)

type Profile struct {
	Name   string      `json:"name"`
	Link   string      `json:"link"`
	Params link.Params `json:"params"`
}

var ErrNoQR = errors.New("no_qr_code")

// FromLink parses a vless:// link (surrounding whitespace is ignored).
func FromLink(s string) (Profile, error) {
	s = strings.TrimSpace(s)
	p, err := link.Parse(s)
	if err != nil {
		return Profile{}, err
	}
	name := p.Name
	if name == "" {
		name = p.Host
	}
	return Profile{Name: name, Link: s, Params: p}, nil
}

// FromQRImage decodes a PNG/JPEG containing a QR code with a vless:// link.
func FromQRImage(path string) (Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return Profile{}, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return Profile{}, err
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return Profile{}, ErrNoQR
	}
	res, err := qrcode.NewQRCodeReader().Decode(bmp, map[gozxing.DecodeHintType]interface{}{gozxing.DecodeHintType_TRY_HARDER: true})
	if err != nil {
		return Profile{}, ErrNoQR
	}
	return FromLink(res.GetText())
}
