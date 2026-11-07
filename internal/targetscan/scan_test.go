package targetscan

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/wayveil/wayveil/internal/targetcheck"
)

func TestScanOrder(t *testing.T) {
	self := netip.MustParseAddr("10.0.1.5")
	o := ScanOrder(self)
	want := []string{"10.0.1.4", "10.0.1.6", "10.0.1.3"}
	for i, w := range want {
		if o[i].String() != w {
			t.Fatalf("o[%d]=%s want %s", i, o[i], w)
		}
	}
	if len(o) != 1023 {
		t.Fatalf("len %d", len(o))
	}
	own := netip.MustParsePrefix("10.0.1.0/24")
	for i, a := range o {
		if a == self {
			t.Fatal("self included")
		}
		if !netip.MustParsePrefix("10.0.0.0/22").Contains(a) {
			t.Fatalf("%s outside /22", a)
		}
		if i < 255 && !own.Contains(a) {
			t.Fatalf("o[%d]=%s should be in own /24", i, a)
		}
	}
	if own.Contains(o[255]) {
		t.Fatalf("o[255]=%s should be outside own /24", o[255])
	}
	if ScanOrder(netip.MustParseAddr("2001:db8::1")) != nil {
		t.Fatal("ipv6 should be nil")
	}
}

// ---- helpers ----

type fakeNet struct {
	mu     sync.Mutex
	byIP   map[string]string // "ip:443" -> listener addr
	byName map[string]string // "domain:443" -> listener addr
	pool   *x509.CertPool
}

func newFakeNet() *fakeNet {
	return &fakeNet{byIP: map[string]string{}, byName: map[string]string{}, pool: x509.NewCertPool()}
}

func (f *fakeNet) dial(m map[string]string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		f.mu.Lock()
		real, ok := m[addr]
		f.mu.Unlock()
		if !ok {
			return nil, errors.New("refused")
		}
		var d net.Dialer
		return d.DialContext(ctx, network, real)
	}
}

func makeCert(t *testing.T, names []string, ips []net.IP) tls.Certificate {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: firstOr(names, "x")},
		DNSNames: names, IPAddresses: ips, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func firstOr(s []string, d string) string {
	if len(s) > 0 {
		return s[0]
	}
	return d
}

// addServer starts a TLS server presenting names at fake IP ip.
func (f *fakeNet) addServer(t *testing.T, ip string, names []string, maxVer uint16) {
	t.Helper()
	cert := makeCert(t, names, nil)
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MaxVersion: maxVer}
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pool.AddCert(leaf)
	f.byIP[ip+":443"] = srv.Listener.Addr().String()
	for _, n := range names {
		f.byName[n+":443"] = srv.Listener.Addr().String()
	}
}

func (f *fakeNet) options() Options {
	return Options{
		Self:  netip.MustParseAddr("10.9.9.1"),
		Check: targetcheck.Options{RootCAs: f.pool, DialContext: f.dial(f.byName)},
		Resolve: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("203.0.113.5")}, nil
		},
		dial: f.dial(f.byIP),
	}
}

func collect(t *testing.T, o Options) []Candidate {
	t.Helper()
	var mu sync.Mutex
	var got []Candidate
	_, err := Scan(context.Background(), o, func(c Candidate) { mu.Lock(); got = append(got, c); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestScanFindsAndVerifies(t *testing.T) {
	f := newFakeNet()
	f.addServer(t, "10.9.9.2", []string{"good.test"}, 0)
	f.addServer(t, "10.9.9.3", []string{"good.test"}, 0)
	f.addServer(t, "10.9.9.4", []string{"old.test"}, tls.VersionTLS12)
	got := collect(t, f.options())
	if len(got) != 1 || got[0].Domain != "good.test" || !got[0].Report.OK {
		t.Fatalf("%+v", got)
	}
}

func TestScanSkipsWildcardAndIPSANs(t *testing.T) {
	f := newFakeNet()
	cert := makeCert(t, []string{"*.wild.test"}, []net.IP{net.ParseIP("10.9.9.2")})
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	f.byIP["10.9.9.2:443"] = srv.Listener.Addr().String()
	if got := collect(t, f.options()); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestScanLimit(t *testing.T) {
	f := newFakeNet()
	for i := 0; i < 10; i++ {
		f.addServer(t, fmt.Sprintf("10.9.9.%d", i+2), []string{fmt.Sprintf("good%d.test", i)}, 0)
	}
	o := f.options()
	o.Limit = 5
	if got := collect(t, o); len(got) != 5 {
		t.Fatalf("got %d", len(got))
	}
}

func TestScanDeadline(t *testing.T) {
	f := newFakeNet()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c // hang: never respond
		}
	}()
	for i := 2; i < 200; i++ {
		f.byIP[fmt.Sprintf("10.9.9.%d:443", i)] = ln.Addr().String()
	}
	o := f.options()
	o.Deadline = 300 * time.Millisecond
	start := time.Now()
	collect(t, o)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("took %v", d)
	}
}
