package targetcheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func startServer(t *testing.T, h http.HandlerFunc, maxVer uint16, h2 bool) (*httptest.Server, Options) {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.TLS = &tls.Config{MaxVersion: maxVer}
	srv.EnableHTTP2 = h2
	srv.StartTLS()
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	addr := srv.Listener.Addr().String()
	return srv, Options{
		RootCAs: pool,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
}

func ok(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }

func TestCheckGood(t *testing.T) {
	_, o := startServer(t, ok, 0, true)
	r := Check(context.Background(), "example.com", o)
	if !r.OK || !r.TLS13 || !r.HTTP2 || len(r.Problems) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestCheckNoTLS13(t *testing.T) {
	_, o := startServer(t, ok, tls.VersionTLS12, true)
	r := Check(context.Background(), "example.com", o)
	if r.OK || !slices.Contains(r.Problems, NoTLS13) {
		t.Fatalf("%+v", r)
	}
}

func TestCheckNoHTTP2(t *testing.T) {
	_, o := startServer(t, ok, 0, false)
	r := Check(context.Background(), "example.com", o)
	if r.OK || !slices.Contains(r.Problems, NoHTTP2) {
		t.Fatalf("%+v", r)
	}
}

func TestCheckRedirectOther(t *testing.T) {
	_, o := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://other.example/", http.StatusMovedPermanently)
	}, 0, true)
	r := Check(context.Background(), "example.com", o)
	if r.RedirectHost != "other.example" || !slices.Contains(r.Problems, RedirectOther) || r.OK {
		t.Fatalf("%+v", r)
	}
}

func TestCheckRedirectSameHost(t *testing.T) {
	_, o := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/ru", http.StatusMovedPermanently)
	}, 0, true)
	r := Check(context.Background(), "example.com", o)
	if !r.OK {
		t.Fatalf("%+v", r)
	}
}

func TestCheckBadCert(t *testing.T) {
	_, o := startServer(t, ok, 0, true)
	o.RootCAs = nil
	r := Check(context.Background(), "example.com", o)
	if r.OK || !slices.Contains(r.Problems, BadCert) {
		t.Fatalf("%+v", r)
	}
}

func TestCheckUnreachable(t *testing.T) {
	r := Check(context.Background(), "example.com", Options{
		DialContext: func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("refused") },
	})
	if !slices.Equal(r.Problems, []Problem{Unreachable}) || r.OK {
		t.Fatalf("%+v", r)
	}
}
