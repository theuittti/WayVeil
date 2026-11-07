// Package targetcheck verifies that a domain is a usable REALITY target:
// TLS 1.3, HTTP/2, a valid certificate, and no redirect to another host.
package targetcheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Problem string

const (
	Unreachable   Problem = "unreachable"
	NoTLS13       Problem = "no_tls13"
	NoHTTP2       Problem = "no_http2"
	RedirectOther Problem = "redirect_other_domain"
	BadCert       Problem = "bad_certificate"
	Slow          Problem = "slow"
)

type Report struct {
	Domain       string    `json:"domain"`
	OK           bool      `json:"ok"`
	TLS13        bool      `json:"tls13"`
	HTTP2        bool      `json:"http2"`
	RedirectHost string    `json:"redirectHost,omitempty"`
	HandshakeMs  int       `json:"handshakeMs"`
	Problems     []Problem `json:"problems"`
}

type Options struct {
	Timeout       time.Duration // default 5s
	Port          int           // default 443
	RootCAs       *x509.CertPool
	DialContext   func(ctx context.Context, network, addr string) (net.Conn, error)
	SlowThreshold time.Duration // default 800ms
}

func (o Options) withDefaults() Options {
	if o.Timeout == 0 {
		o.Timeout = 5 * time.Second
	}
	if o.Port == 0 {
		o.Port = 443
	}
	if o.DialContext == nil {
		o.DialContext = (&net.Dialer{}).DialContext
	}
	if o.SlowThreshold == 0 {
		o.SlowThreshold = 800 * time.Millisecond
	}
	return o
}

// Check runs the REALITY target checks against domain.
func Check(ctx context.Context, domain string, o Options) Report {
	o = o.withDefaults()
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	r := Report{Domain: domain, Problems: []Problem{}}
	addr := net.JoinHostPort(domain, strconv.Itoa(o.Port))

	// One handshake (TLS 1.2+ so we can tell "old TLS" from "unreachable");
	// certificate is verified manually afterwards.
	start := time.Now()
	raw, err := o.DialContext(ctx, "tcp", addr)
	if err != nil {
		r.Problems = []Problem{Unreachable}
		return r
	}
	conn := tls.Client(raw, &tls.Config{
		ServerName: domain, MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"}, InsecureSkipVerify: true,
	})
	if err := conn.HandshakeContext(ctx); err != nil {
		raw.Close()
		r.Problems = []Problem{Unreachable}
		return r
	}
	r.HandshakeMs = int(time.Since(start).Milliseconds())
	st := conn.ConnectionState()
	conn.Close()

	r.TLS13 = st.Version == tls.VersionTLS13
	r.HTTP2 = st.NegotiatedProtocol == "h2"
	if !r.TLS13 {
		r.Problems = append(r.Problems, NoTLS13)
	}
	if !r.HTTP2 {
		r.Problems = append(r.Problems, NoHTTP2)
	}
	if !certValid(st.PeerCertificates, domain, o.RootCAs) {
		r.Problems = append(r.Problems, BadCert)
	}

	host, err := redirectHost(ctx, domain, o)
	switch {
	case err != nil:
		r.Problems = append(r.Problems, Unreachable)
	case host != "" && !strings.EqualFold(host, domain):
		r.RedirectHost = host
		r.Problems = append(r.Problems, RedirectOther)
	}
	if time.Duration(r.HandshakeMs)*time.Millisecond > o.SlowThreshold {
		r.Problems = append(r.Problems, Slow)
	}
	r.OK = true
	for _, p := range r.Problems {
		if p != Slow {
			r.OK = false
		}
	}
	return r
}

func certValid(chain []*x509.Certificate, domain string, roots *x509.CertPool) bool {
	if len(chain) == 0 {
		return false
	}
	inter := x509.NewCertPool()
	for _, c := range chain[1:] {
		inter.AddCert(c)
	}
	_, err := chain[0].Verify(x509.VerifyOptions{DNSName: domain, Roots: roots, Intermediates: inter})
	return err == nil
}

// redirectHost GETs https://domain/ without following redirects and returns
// the Location host of a 3xx response ("" when there is no redirect).
func redirectHost(ctx context.Context, domain string, o Options) (string, error) {
	tr := &http.Transport{
		DialContext:       o.DialContext,
		TLSClientConfig:   &tls.Config{ServerName: domain, InsecureSkipVerify: true},
		ForceAttemptHTTP2: true,
	}
	defer tr.CloseIdleConnections()
	cl := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	u := "https://" + domain + "/"
	if o.Port != 443 {
		u = "https://" + net.JoinHostPort(domain, strconv.Itoa(o.Port)) + "/"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36")
	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", nil
	}
	loc, err := resp.Location()
	if err != nil {
		return "", nil
	}
	return loc.Hostname(), nil
}
