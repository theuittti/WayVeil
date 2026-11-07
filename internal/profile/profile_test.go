package profile

import (
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"

	"github.com/theuittti/WayVeil/internal/link"
)

var params = link.Params{UUID: "11111111-2222-3333-4444-555555555555", Host: "203.0.113.10", Port: 443,
	SNI: "www.nu.nl", PublicKey: "PBK", ShortID: "ab12", Fingerprint: "chrome", Flow: "xtls-rprx-vision", Name: "Вася"}

func TestFromLink(t *testing.T) {
	p, err := FromLink("\n  " + link.Build(params) + "  \n")
	if err != nil || p.Name != "Вася" || p.Params != params || p.Link != link.Build(params) {
		t.Fatalf("%+v %v", p, err)
	}
	noName := params
	noName.Name = ""
	if p, _ := FromLink(link.Build(noName)); p.Name != "203.0.113.10" {
		t.Fatalf("name fallback %q", p.Name)
	}
}

func TestFromLinkInvalid(t *testing.T) {
	if _, err := FromLink("vmess://abc"); !errors.Is(err, link.ErrNotVless) {
		t.Fatalf("%v", err)
	}
}

func savePNG(t *testing.T, img image.Image) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "qr.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFromQRImage(t *testing.T) {
	bm, err := qrcode.NewQRCodeWriter().Encode(link.Build(params), gozxing.BarcodeFormat_QR_CODE, 300, 300, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := FromQRImage(savePNG(t, bm))
	if err != nil || p.Params != params {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestFromQRImageNoCode(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 200, 200))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	if _, err := FromQRImage(savePNG(t, img)); !errors.Is(err, ErrNoQR) {
		t.Fatalf("%v", err)
	}
}
