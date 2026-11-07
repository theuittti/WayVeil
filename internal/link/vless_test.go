package link

import (
	"errors"
	"testing"
)

func TestBuildExact(t *testing.T) {
	got := Build(Params{
		UUID: "11111111-2222-3333-4444-555555555555", Host: "203.0.113.10",
		SNI: "www.example.nl", PublicKey: "PBK", ShortID: "ab12", Name: "Вася ноутбук",
	})
	want := "vless://11111111-2222-3333-4444-555555555555@203.0.113.10:443?flow=xtls-rprx-vision&fp=chrome&pbk=PBK&security=reality&sid=ab12&sni=www.example.nl&type=tcp#%D0%92%D0%B0%D1%81%D1%8F%20%D0%BD%D0%BE%D1%83%D1%82%D0%B1%D1%83%D0%BA"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestRoundTripQuotes(t *testing.T) {
	p := Params{UUID: "u-1", Host: "h.example", Port: 443, SNI: "s", PublicKey: "k", ShortID: "01",
		Fingerprint: "chrome", Flow: "xtls-rprx-vision", Name: `O'Brien "x"`}
	got, err := Parse(Build(p))
	if err != nil {
		t.Fatal(err)
	}
	if got != p {
		t.Fatalf("got %+v want %+v", got, p)
	}
}

func TestParseIPv6Host(t *testing.T) {
	s := "vless://u@[2001:db8::1]:443?security=reality&sni=a&pbk=b&sid=c&type=tcp&flow=xtls-rprx-vision#n"
	p, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "2001:db8::1" {
		t.Fatalf("host %q", p.Host)
	}
	if b := Build(p); b[:len("vless://u@[2001:db8::1]:443")] != "vless://u@[2001:db8::1]:443" {
		t.Fatalf("build %s", b)
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse("vmess://x"); !errors.Is(err, ErrNotVless) {
		t.Fatalf("want ErrNotVless, got %v", err)
	}
	_, err := Parse("vless://u@h:443?security=reality&sni=a&sid=c&type=tcp#n")
	var mf ErrMissingField
	if !errors.As(err, &mf) || mf.Field != "pbk" {
		t.Fatalf("want missing pbk, got %v", err)
	}
}
