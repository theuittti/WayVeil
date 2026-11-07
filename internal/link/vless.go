// Package link builds and parses VLESS + REALITY share links.
package link

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
)

// Params are the fields of a VLESS + REALITY + Vision link.
type Params struct {
	UUID, Host  string
	Port        int
	SNI         string
	PublicKey   string
	ShortID     string
	Fingerprint string
	Flow        string
	Name        string
}

var ErrNotVless = errors.New("not a vless:// link")

// ErrMissingField reports a required link field that is absent.
type ErrMissingField struct{ Field string }

func (e ErrMissingField) Error() string { return "link: missing field " + e.Field }

func (p Params) withDefaults() Params {
	if p.Port == 0 {
		p.Port = 443
	}
	if p.Fingerprint == "" {
		p.Fingerprint = "chrome"
	}
	if p.Flow == "" {
		p.Flow = "xtls-rprx-vision"
	}
	return p
}

// Build returns the vless:// share link for p.
func Build(p Params) string {
	p = p.withDefaults()
	q := url.Values{}
	q.Set("security", "reality")
	q.Set("sni", p.SNI)
	q.Set("fp", p.Fingerprint)
	q.Set("pbk", p.PublicKey)
	q.Set("sid", p.ShortID)
	q.Set("flow", p.Flow)
	q.Set("type", "tcp")
	return fmt.Sprintf("vless://%s@%s?%s#%s", p.UUID,
		net.JoinHostPort(p.Host, strconv.Itoa(p.Port)), q.Encode(), url.PathEscape(p.Name))
}

// Parse decodes a vless:// share link.
func Parse(s string) (Params, error) {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "vless" {
		return Params{}, ErrNotVless
	}
	q := u.Query()
	p := Params{
		UUID: u.User.Username(), Host: u.Hostname(), SNI: q.Get("sni"), PublicKey: q.Get("pbk"),
		ShortID: q.Get("sid"), Fingerprint: q.Get("fp"), Flow: q.Get("flow"), Name: u.Fragment,
	}
	if p.UUID == "" {
		return Params{}, ErrMissingField{"uuid"}
	}
	if p.Host == "" {
		return Params{}, ErrMissingField{"host"}
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 {
		return Params{}, ErrMissingField{"port"}
	}
	p.Port = port
	if q.Get("security") != "reality" {
		return Params{}, ErrMissingField{"security"}
	}
	for _, f := range []struct{ name, v string }{{"sni", p.SNI}, {"pbk", p.PublicKey}} {
		if f.v == "" {
			return Params{}, ErrMissingField{f.name}
		}
	}
	if q.Get("type") != "tcp" {
		return Params{}, ErrMissingField{"type"}
	}
	return p.withDefaults(), nil
}
