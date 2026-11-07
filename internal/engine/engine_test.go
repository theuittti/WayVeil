package engine

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/link"
	"github.com/theuittti/WayVeil/internal/mihomo"
	"github.com/theuittti/WayVeil/internal/profile"
	"github.com/theuittti/WayVeil/internal/xrayclient"
)

var bg = context.Background()

// hangingServer accepts TCP and never answers: the "VPN server" for tests,
// so proxied connections stay open long enough to be observed.
func hangingServer(t *testing.T) int {
	t.Helper()
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
			_ = c
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// closingServer accepts TCP and closes at once: the proxy dial fails fast
// and mihomo logs the failed connection with the rule that matched.
func closingServer(t *testing.T) int {
	t.Helper()
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
			c.Close()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func freePort(t *testing.T) int {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func setup(t *testing.T) (clientstore.Settings, Options) {
	t.Helper()
	if _, err := mihomo.Find(); err != nil {
		t.Skip("run make mihomo")
	}
	if _, err := xrayclient.Find(); err != nil {
		t.Skip("run make xray")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p := link.Params{UUID: "11111111-2222-3333-4444-555555555555", Host: "127.0.0.1", Port: hangingServer(t),
		SNI: "www.nu.nl", PublicKey: "KjRhl1CVm-XAfrJdTiWyC3zui05Tcg2bMNRBZOH0CUM", ShortID: "9e5ee700"}
	s := clientstore.Settings{Mode: clientstore.ModeSmart, Profile: &profile.Profile{Name: "test", Params: p}}
	return s, Options{Inbound: mihomo.Mixed, MixedPort: freePort(t)}
}

func connect(t *testing.T, s clientstore.Settings, o Options) *Engine {
	t.Helper()
	e, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Connect(bg, s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Disconnect(bg) })
	return e
}

func TestConnectStatusDisconnect(t *testing.T) {
	s, o := setup(t)
	e := connect(t, s, o)
	st := e.Status(bg)
	if st.State != Connected || st.Server != "test" || st.Mode != clientstore.ModeSmart {
		t.Fatalf("%+v", st)
	}
	run := filepath.Join(e.dir, "run.json")
	if fi, err := os.Stat(run); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("run.json %v %v", fi, err)
	}
	pid := e.pid()
	if err := e.Disconnect(bg); err != nil {
		t.Fatal(err)
	}
	if st := e.Status(bg); st.State != Disconnected {
		t.Fatalf("%+v", st)
	}
	if alive(pid) {
		t.Fatal("mihomo still running")
	}
	if err := e.Disconnect(bg); err != nil {
		t.Fatalf("second disconnect: %v", err)
	}
}

// mihomo lists a proxied connection in the Clash API only after the proxy
// handshake, which never completes against the fake server, so the routing
// decision is read from mihomo's info log. Real traffic: routing_integration_test.go.
func TestRoutingDecision(t *testing.T) {
	s, o := setup(t)
	o.LogLevel = "info"
	s.Profile.Params.Port = closingServer(t)
	e := connect(t, s, o)
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer local.Close()
	proxyURL, _ := url.Parse("socks5h://127.0.0.1:" + strconv.Itoa(o.MixedPort))
	cl := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	go cl.Get("http://youtube.com/") // proxied: the fake server drops it
	if _, err := cl.Get(local.URL); err != nil {
		t.Fatalf("direct request: %v", err)
	}
	if _, err := e.clash(); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`RuleSet[(/]ru-blocked.*youtube\.com:80|youtube\.com:80.*RuleSet[(/]ru-blocked`)
	reDirect := regexp.MustCompile(`--> 127\.0\.0\.1:\d+ match IPCIDR\(127\.0\.0\.0/8\) using DIRECT`)
	deadline := time.Now().Add(3 * time.Second)
	var log []byte
	for time.Now().Before(deadline) {
		log, _ = os.ReadFile(filepath.Join(e.dir, "mihomo.log"))
		if re.Match(log) && reDirect.Match(log) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("routing decisions not found in log:\n%s", log)
}

func TestProcessDiedReported(t *testing.T) {
	s, o := setup(t)
	e := connect(t, s, o)
	syscall.Kill(e.pid(), syscall.SIGKILL)
	time.Sleep(300 * time.Millisecond)
	if st := e.Status(bg); st.State != Failed || st.Error == "" {
		t.Fatalf("%+v", st)
	}
	if err := e.Disconnect(bg); err != nil {
		t.Fatal(err)
	}
	if err := e.Connect(bg, s); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
}

func TestXrayDiedReported(t *testing.T) {
	s, o := setup(t)
	e := connect(t, s, o)
	r, _ := e.readRun()
	if !alive(r.XrayPID) {
		t.Fatal("xray not running")
	}
	syscall.Kill(r.XrayPID, syscall.SIGKILL)
	time.Sleep(300 * time.Millisecond)
	if st := e.Status(bg); st.State != Failed || !strings.Contains(st.Error, "xray") {
		t.Fatalf("%+v", st)
	}
	if err := e.Disconnect(bg); err != nil {
		t.Fatal(err)
	}
	if alive(r.PID) {
		t.Fatal("mihomo must be stopped too")
	}
}

func TestStaleProcessReplaced(t *testing.T) {
	s, o := setup(t)
	first := connect(t, s, o)
	old := first.pid()
	second := connect(t, s, o)
	if alive(old) {
		t.Fatal("old mihomo still running")
	}
	if second.Status(bg).State != Connected {
		t.Fatal("second not connected")
	}
}

func TestConnectWithoutProfile(t *testing.T) {
	_, o := setup(t)
	e, _ := New(o)
	if err := e.Connect(bg, clientstore.Settings{}); err != ErrNoProfile {
		t.Fatalf("%v", err)
	}
}
