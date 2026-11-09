package gui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/ownerstore"
	"github.com/theuittti/WayVeil/internal/remote"
	"github.com/theuittti/WayVeil/internal/server"
	"github.com/theuittti/WayVeil/internal/sshx"
	"github.com/theuittti/WayVeil/internal/targetcheck"
	"github.com/theuittti/WayVeil/rules"
)

// ---- fakes ----

type fakeConn struct{ closed atomic.Bool }

func (c *fakeConn) Run(context.Context, string, []byte) ([]byte, error)        { return nil, nil }
func (c *fakeConn) Stream(context.Context, string, []byte, func(string)) error { return nil }
func (c *fakeConn) Fingerprint() string                                        { return "SHA256:fakefp" }
func (c *fakeConn) HostKey() string                                            { return "ssh-ed25519 AAAAfakehostkey" }
func (c *fakeConn) Close() error                                               { c.closed.Store(true); return nil }

type event struct {
	name string
	data any
}

type recEmitter struct {
	mu     sync.Mutex
	events []event
}

func (r *recEmitter) Emit(name string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event{name, data})
}

func (r *recEmitter) all() []event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]event(nil), r.events...)
}

func (r *recEmitter) count(name string) int {
	n := 0
	for _, e := range r.all() {
		if e.name == name {
			n++
		}
	}
	return n
}

func (r *recEmitter) waitFor(t *testing.T, name string, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for r.count(name) < n {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d %q events (have %d)", n, name, r.count(name))
		}
		time.Sleep(2 * time.Millisecond)
	}
}

type harness struct {
	app     *App
	em      *recEmitter
	d       *Deps
	conn    *fakeConn
	cfgDir  string
	mu      sync.Mutex
	calls   []string
	targets []server.Suggestion
	lastTgt sshx.Target
}

func (h *harness) rec(s string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, s)
}

func (h *harness) callList() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.calls...)
}

// newHarness builds fake deps; tweak h.d before calling h.start().
func newHarness(t *testing.T, info server.HostInfo) *harness {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", t.TempDir())
	h := &harness{em: &recEmitter{}, conn: &fakeConn{}, cfgDir: cfg}
	h.d = &Deps{
		Connect: func(_ context.Context, tg sshx.Target) (Conn, error) {
			h.rec("connect")
			h.mu.Lock()
			h.lastTgt = tg
			h.mu.Unlock()
			return h.conn, nil
		},
		Precheck: func(context.Context, remote.Runner) (server.HostInfo, error) {
			h.rec("precheck")
			return info, nil
		},
		SuggestTargets: func(_ context.Context, _ remote.Runner, _ server.HostInfo, _ rules.Targets, on func(server.Suggestion)) error {
			h.rec("suggest")
			for _, s := range h.targets {
				on(s)
			}
			return nil
		},
		CheckTarget: func(_ context.Context, _ remote.Runner, _, domain string) (targetcheck.Report, error) {
			return targetcheck.Report{Domain: domain, OK: true, TLS13: true, HTTP2: true}, nil
		},
		CleanupScanner: func(context.Context, remote.Runner) error { return nil },
		Install: func(_ context.Context, _ remote.Runner, o server.InstallOptions, on func(server.StepEvent)) (server.InstallResult, error) {
			h.rec("install:" + string(o.XrayZip))
			on(server.StepEvent{ID: "authkey", Status: server.StepStart})
			on(server.StepEvent{ID: "authkey", Status: server.StepOK})
			return server.InstallResult{PublicKey: "pbk", ShortID: "ab"}, nil
		},
		Uninstall: func(_ context.Context, _ remote.Runner, _ string, on func(server.StepEvent)) error {
			h.rec("uninstall")
			on(server.StepEvent{ID: "service", Status: server.StepStart})
			on(server.StepEvent{ID: "service", Status: server.StepOK})
			return nil
		},
		Adopt: func(_ context.Context, _ remote.Runner, user, line string) error {
			h.rec("adopt:" + user + ":" + line)
			return nil
		},
		AddClient: func(_ context.Context, _ remote.Runner, name string) (server.Client, error) {
			h.rec("addclient:" + name)
			return server.Client{ID: "c-1", UUID: "11111111-2222-3333-4444-555555555555", Name: name}, nil
		},
		Info: func(context.Context, remote.Runner) (server.ServerInfo, error) {
			h.rec("info")
			return server.ServerInfo{Address: "1.2.3.4", SNI: "www.microsoft.com", PublicKey: "pbk", ShortID: "ab"}, nil
		},
		GenerateKey: func() ([]byte, string, error) {
			h.rec("genkey")
			return []byte("PRIVATE-PEM"), "ssh-ed25519 AAAAappkey wayveil", nil
		},
		LoadOwner: ownerstore.Load,
		SaveOwner: func(p ownerstore.ServerProfile) error {
			h.rec("saveowner")
			return ownerstore.Save(p)
		},
		LoadSettings: clientstore.Load,
		SaveSettings: clientstore.Save,
		Targets:      func() rules.Targets { return rules.Targets{} },
		XrayZip:      func(string) []byte { return nil },
		Now:          func() time.Time { return time.UnixMilli(1000) },
	}
	return h
}

func (h *harness) start(t *testing.T) *App {
	t.Helper()
	h.app = New(h.em, *h.d)
	t.Cleanup(h.app.Shutdown)
	return h.app
}

func (h *harness) connect(t *testing.T) HostCheck {
	t.Helper()
	hc, err := h.app.ServerConnect(AccessForm{Host: "1.2.3.4", User: "root", Password: "pw"})
	if err != nil {
		t.Fatalf("ServerConnect: %v", err)
	}
	return hc
}

func freshInfo() server.HostInfo {
	return server.HostInfo{OSID: "ubuntu", OSVersion: "24.04", Arch: "amd64", Supported: true, PublicIP: "1.2.3.4"}
}

func scanActive(a *App) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.scanCancel != nil
}

// ---- tests ----

func TestServerConnectStates(t *testing.T) {
	cases := []struct {
		name  string
		mod   func(*server.HostInfo)
		state string
		scan  bool
	}{
		{"fresh", func(*server.HostInfo) {}, "fresh", true},
		{"unsupported", func(h *server.HostInfo) { h.Supported = false; h.OSID = "centos"; h.OSVersion = "7" }, "unsupported", false},
		{"partial", func(h *server.HostInfo) { h.PartialInstall = true; h.HasWayveil = true }, "partial", true},
		{"wayveil", func(h *server.HostInfo) { h.HasWayveil = true; h.Port443Busy = true }, "wayveil", true},
		{"foreign", func(h *server.HostInfo) { h.HasForeignXray = true; h.Port443Busy = true }, "foreign", false},
		{"port-busy", func(h *server.HostInfo) { h.Port443Busy = true; h.Port443Process = "nginx" }, "port-busy", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info := freshInfo()
			c.mod(&info)
			h := newHarness(t, info)
			started := make(chan struct{}, 1)
			h.d.SuggestTargets = func(ctx context.Context, _ remote.Runner, _ server.HostInfo, _ rules.Targets, _ func(server.Suggestion)) error {
				started <- struct{}{}
				<-ctx.Done()
				return ctx.Err()
			}
			h.start(t)
			hc := h.connect(t)
			if hc.State != c.state {
				t.Fatalf("state = %q, want %q", hc.State, c.state)
			}
			if hc.Fingerprint != "SHA256:fakefp" || hc.Pinned || hc.Arch != "amd64" {
				t.Fatalf("hostcheck = %+v", hc)
			}
			if c.state == "port-busy" && hc.Detail != "nginx" {
				t.Fatalf("detail = %q", hc.Detail)
			}
			if c.state == "unsupported" && hc.Detail != "centos 7" {
				t.Fatalf("detail = %q", hc.Detail)
			}
			if got := scanActive(h.app); got != c.scan {
				t.Fatalf("scan active = %v, want %v", got, c.scan)
			}
			if c.scan {
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("scan not started")
				}
			}
			h.app.ServerDisconnect()
			if !h.conn.closed.Load() {
				t.Fatal("ServerDisconnect did not close the connection")
			}
			if len(started) != 0 {
				t.Fatal("scan started for a refused state")
			}
		})
	}
}

func TestServerConnectSavedProfile(t *testing.T) {
	h := newHarness(t, freshInfo())
	h.start(t)
	if err := ownerstore.Save(ownerstore.ServerProfile{Host: "1.2.3.4", Port: 22, User: "admin",
		HostKey: "ssh-ed25519 AAAAsaved", PrivateKeyPEM: []byte("SAVED-PEM"), AuthorizedLine: "l"}); err != nil {
		t.Fatal(err)
	}
	hc, err := h.app.ServerConnect(AccessForm{Host: "1.2.3.4"})
	if err != nil {
		t.Fatal(err)
	}
	if !hc.Pinned {
		t.Fatal("want Pinned for a saved host key")
	}
	h.mu.Lock()
	tg := h.lastTgt
	h.mu.Unlock()
	if tg.HostKey != "ssh-ed25519 AAAAsaved" || string(tg.PrivateKeyPEM) != "SAVED-PEM" || tg.User != "admin" || tg.Password != "" {
		t.Fatalf("target = %+v", tg)
	}
	// With a password the saved host key is still pinned, but the password is used.
	if _, err := h.app.ServerConnect(AccessForm{Host: "1.2.3.4", User: "root", Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	tg = h.lastTgt
	h.mu.Unlock()
	if tg.HostKey != "ssh-ed25519 AAAAsaved" || tg.Password != "pw" || len(tg.PrivateKeyPEM) != 0 || tg.User != "root" {
		t.Fatalf("target = %+v", tg)
	}
}

func TestParseHost(t *testing.T) {
	ok := []struct {
		in   string
		host string
		port int
		user string
	}{
		{" 1.2.3.4 ", "1.2.3.4", 22, "root"},
		{"root@1.2.3.4", "1.2.3.4", 22, "root"},
		{"ssh://admin@1.2.3.4:2222", "1.2.3.4", 2222, "admin"},
		{"ssh://1.2.3.4:2222", "1.2.3.4", 2222, "root"},
		{"[2001:db8::1]:22", "2001:db8::1", 22, "root"},
		{"[2001:db8::1]", "2001:db8::1", 22, "root"},
		{"2001:db8::1", "2001:db8::1", 22, "root"},
		{"vps.example.com\n", "vps.example.com", 22, "root"},
	}
	for _, c := range ok {
		h, p, u, err := parseHost(c.in, 0, "")
		if err != nil || h != c.host || p != c.port || u != c.user {
			t.Errorf("parseHost(%q) = %q %d %q %v", c.in, h, p, u, err)
		}
	}
	if _, p, u, _ := parseHost("1.2.3.4", 2200, " admin "); p != 2200 || u != "admin" {
		t.Errorf("form port/user not used: %d %q", p, u)
	}
	for _, in := range []string{"", "  ", "a b", "1.2.3.4:99999", "http://1.2.3.4/x", "@1.2.3.4", "-x"} {
		_, _, _, err := parseHost(in, 0, "")
		if err == nil {
			t.Errorf("parseHost(%q): want error", in)
			continue
		}
		if got := Human(err); got != "Enter the server's IP address or hostname." {
			t.Errorf("Human = %q", got)
		}
	}
	h := newHarness(t, freshInfo())
	h.start(t)
	_, err := h.app.ServerConnect(AccessForm{Host: "a b"})
	if err == nil || err.Error() != "Enter the server's IP address or hostname." {
		t.Fatalf("ServerConnect bad host err = %v", err)
	}
}

func TestPasswordNeverLeaks(t *testing.T) {
	const pw = "S3cr3t-pw"
	h := newHarness(t, freshInfo())
	h.targets = []server.Suggestion{{Kind: server.KindNearby, Domain: "a.example", Recommended: true}}
	fail := true
	h.d.Connect = func(_ context.Context, tg sshx.Target) (Conn, error) {
		if tg.Password != pw {
			return nil, errors.New("password not passed")
		}
		if fail {
			return nil, sshx.ErrAuth
		}
		return h.conn, nil
	}
	h.d.Install = func(_ context.Context, _ remote.Runner, _ server.InstallOptions, on func(server.StepEvent)) (server.InstallResult, error) {
		on(server.StepEvent{ID: "deps", Status: server.StepStart})
		on(server.StepEvent{ID: "deps", Status: server.StepFail})
		return server.InstallResult{}, &server.ScriptError{Code: "install_failed", Message: "step deps failed (exit 100): apt"}
	}
	a := h.start(t)
	var errs []error
	_, err := a.ServerConnect(AccessForm{Host: "1.2.3.4", Password: pw})
	errs = append(errs, err)
	fail = false
	hc, err := a.ServerConnect(AccessForm{Host: "1.2.3.4", Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	h.em.waitFor(t, "target-done", 1)
	_, err = a.Install("www.microsoft.com")
	if err == nil {
		t.Fatal("want install error")
	}
	errs = append(errs, err)
	for _, e := range errs {
		if e == nil {
			t.Fatal("missing error")
		}
		if strings.Contains(e.Error(), pw) || strings.Contains(Human(e), pw) {
			t.Fatalf("error leaks password: %v", e)
		}
	}
	if s := fmt.Sprintf("%+v", hc); strings.Contains(s, pw) {
		t.Fatalf("hostcheck leaks password: %s", s)
	}
	for _, e := range h.em.all() {
		if s := fmt.Sprintf("%+v", e.data); strings.Contains(s, pw) {
			t.Fatalf("event %s leaks password: %s", e.name, s)
		}
	}
	err = filepath.WalkDir(h.cfgDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), pw) {
			t.Errorf("%s contains the password", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInstallOrder(t *testing.T) {
	for _, c := range []struct{ arch, zip string }{{"amd64", "ZIP-AMD64"}, {"arm64", "ZIP-ARM64"}} {
		t.Run(c.arch, func(t *testing.T) {
			info := freshInfo()
			info.Arch = c.arch
			h := newHarness(t, info)
			exe := t.TempDir()
			os.WriteFile(filepath.Join(exe, "Xray-linux-64.zip"), []byte("ZIP-AMD64"), 0o644)
			os.WriteFile(filepath.Join(exe, "Xray-linux-arm64-v8a.zip"), []byte("ZIP-ARM64"), 0o644)
			h.d.XrayZip = zipReader(exe)
			a := h.start(t)
			h.connect(t)
			done, err := a.Install(" www.microsoft.com ")
			if err != nil {
				t.Fatal(err)
			}
			var order []string
			for _, s := range h.callList() {
				if s != "suggest" && s != "connect" && s != "precheck" {
					order = append(order, s)
				}
			}
			want := []string{"genkey", "saveowner", "install:" + c.zip, "addclient:Me", "info"}
			if strings.Join(order, ",") != strings.Join(want, ",") {
				t.Fatalf("calls = %v, want %v", order, want)
			}
			if !strings.HasPrefix(done.Link, "vless://") || !strings.HasPrefix(done.QR, "data:image/png;base64,") {
				t.Fatalf("done = %.80s / %.40s", done.Link, done.QR)
			}
			p, err := ownerstore.Load("1.2.3.4", 22)
			if err != nil || p.HostKey != "ssh-ed25519 AAAAfakehostkey" || string(p.PrivateKeyPEM) != "PRIVATE-PEM" ||
				p.AuthorizedLine != "ssh-ed25519 AAAAappkey wayveil" || p.User != "root" {
				t.Fatalf("owner profile = %+v, %v", p, err)
			}
			st, err := clientstore.Load()
			if err != nil || st.Profile == nil || st.Profile.Link != done.Link || st.Profile.Name != "Me" {
				t.Fatalf("client profile = %+v, %v", st.Profile, err)
			}
		})
	}
}

func TestZipReaderMissing(t *testing.T) {
	r := zipReader(t.TempDir())
	if r("amd64") != nil || r("arm64") != nil || r("riscv") != nil {
		t.Fatal("missing zip must give nil")
	}
}

func TestInstallBusy(t *testing.T) {
	h := newHarness(t, freshInfo())
	entered := make(chan struct{})
	release := make(chan struct{})
	var n atomic.Int32
	h.d.Install = func(context.Context, remote.Runner, server.InstallOptions, func(server.StepEvent)) (server.InstallResult, error) {
		n.Add(1)
		close(entered)
		<-release
		return server.InstallResult{}, nil
	}
	a := h.start(t)
	h.connect(t)
	errc := make(chan error, 1)
	go func() { _, err := a.Install("www.microsoft.com"); errc <- err }()
	<-entered
	if _, err := a.Install("www.microsoft.com"); !errors.Is(err, ErrBusy) || err.Error() != "Already working on it…" {
		t.Fatalf("second Install err = %v", err)
	}
	if _, err := a.ServerConnect(AccessForm{Host: "1.2.3.4", Password: "pw"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("ServerConnect while installing err = %v", err)
	}
	if _, err := a.Adopt(); !errors.Is(err, ErrBusy) {
		t.Fatalf("Adopt while installing err = %v", err)
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if n.Load() != 1 {
		t.Fatalf("Install dep called %d times", n.Load())
	}
}

func TestInstallStreamDrop(t *testing.T) {
	h := newHarness(t, freshInfo())
	h.d.Install = func(_ context.Context, _ remote.Runner, _ server.InstallOptions, on func(server.StepEvent)) (server.InstallResult, error) {
		on(server.StepEvent{ID: "authkey", Status: server.StepStart})
		on(server.StepEvent{ID: "deps", Status: server.StepStart})
		return server.InstallResult{}, io.ErrUnexpectedEOF
	}
	a := h.start(t)
	h.connect(t)
	_, err := a.Install("www.microsoft.com")
	if err == nil || Human(err) == "" || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v", err)
	}
	var titles []string
	for _, e := range h.em.all() {
		if e.name == "step" {
			s := e.data.(StepEvent)
			titles = append(titles, s.Title)
			if s.At != 1000 || s.Status != "start" {
				t.Fatalf("step = %+v", s)
			}
		}
	}
	if strings.Join(titles, ",") != "App access key,Packages" {
		t.Fatalf("titles = %v", titles)
	}
	// The busy flag is released after a failure.
	if _, err := a.ServerConnect(AccessForm{Host: "1.2.3.4", Password: "pw"}); err != nil {
		t.Fatal(err)
	}
}

func TestInstallScriptErrorNamesStep(t *testing.T) {
	h := newHarness(t, freshInfo())
	h.d.Install = func(_ context.Context, _ remote.Runner, _ server.InstallOptions, on func(server.StepEvent)) (server.InstallResult, error) {
		on(server.StepEvent{ID: "xray", Status: server.StepStart})
		on(server.StepEvent{ID: "xray", Status: server.StepFail})
		return server.InstallResult{}, &server.ScriptError{Code: "install_failed", Message: "digest mismatch"}
	}
	a := h.start(t)
	h.connect(t)
	_, err := a.Install("www.microsoft.com")
	if err == nil || err.Error() != "Installation failed at step Xray: digest mismatch" {
		t.Fatalf("err = %v", err)
	}
}

func slowScan(h *harness, cancelled *atomic.Bool, finished chan struct{}) {
	h.d.SuggestTargets = func(ctx context.Context, _ remote.Runner, _ server.HostInfo, _ rules.Targets, on func(server.Suggestion)) error {
		defer close(finished)
		on(server.Suggestion{Kind: server.KindCountry, Domain: "first.example"})
		<-ctx.Done()
		cancelled.Store(true)
		for i := 0; i < 3; i++ { // late results from a scan that ignores cancellation
			on(server.Suggestion{Kind: server.KindNearby, Domain: fmt.Sprintf("late%d.example", i), Recommended: true})
			time.Sleep(time.Millisecond)
		}
		return ctx.Err()
	}
}

func TestScanCancelledNoLateEvents(t *testing.T) {
	t.Run("install", func(t *testing.T) {
		h := newHarness(t, freshInfo())
		var cancelled atomic.Bool
		finished := make(chan struct{})
		slowScan(h, &cancelled, finished)
		a := h.start(t)
		h.connect(t)
		h.em.waitFor(t, "target", 1)
		if _, err := a.Install("www.microsoft.com"); err != nil {
			t.Fatal(err)
		}
		<-finished
		if !cancelled.Load() {
			t.Fatal("scan context not cancelled")
		}
		if n := h.em.count("target"); n != 1 {
			t.Fatalf("target events = %d, want 1", n)
		}
		if n := h.em.count("target-done"); n != 0 {
			t.Fatalf("target-done after cancel = %d", n)
		}
	})
	t.Run("disconnect", func(t *testing.T) {
		h := newHarness(t, freshInfo())
		var cancelled atomic.Bool
		finished := make(chan struct{})
		slowScan(h, &cancelled, finished)
		a := h.start(t)
		h.connect(t)
		h.em.waitFor(t, "target", 1)
		a.ServerDisconnect()
		select {
		case <-finished: // Disconnect waits for the scan (scanner cleanup) before closing SSH
		default:
			t.Fatal("ServerDisconnect returned before the scan finished")
		}
		if !cancelled.Load() || h.em.count("target") != 1 || h.em.count("target-done") != 0 {
			t.Fatalf("cancelled=%v targets=%d", cancelled.Load(), h.em.count("target"))
		}
	})
}

func TestTargetEventShape(t *testing.T) {
	h := newHarness(t, freshInfo())
	h.targets = []server.Suggestion{{Kind: server.KindNearby, Domain: "n.example", Recommended: true,
		Report: targetcheck.Report{HandshakeMs: 42}}}
	h.start(t)
	h.connect(t)
	h.em.waitFor(t, "target-done", 1)
	for _, e := range h.em.all() {
		if e.name == "target" {
			want := TargetEvent{Domain: "n.example", Kind: "nearby", Recommended: true, HandshakeMs: 42}
			if e.data.(TargetEvent) != want {
				t.Fatalf("target = %+v", e.data)
			}
			return
		}
	}
	t.Fatal("no target event")
}

func TestCheckDomainAlternatives(t *testing.T) {
	h := newHarness(t, freshInfo())
	h.targets = []server.Suggestion{
		{Kind: server.KindCountry, Domain: "a.example"},
		{Kind: server.KindNearby, Domain: "b.example", Recommended: true},
		{Kind: server.KindRussian, Domain: "c.example"},
		{Kind: server.KindNearby, Domain: "d.example", Recommended: true},
		{Kind: server.KindCountry, Domain: "a.example", Recommended: true}, // re-emitted as best
		{Kind: server.KindNearby, Domain: "bad.example"},
	}
	h.d.CheckTarget = func(_ context.Context, _ remote.Runner, arch, domain string) (targetcheck.Report, error) {
		if domain == "good.example" {
			return targetcheck.Report{Domain: domain, OK: true}, nil
		}
		return targetcheck.Report{Domain: domain, OK: false, RedirectHost: "other.example",
			Problems: []targetcheck.Problem{targetcheck.NoTLS13, targetcheck.RedirectOther}}, nil
	}
	a := h.start(t)
	h.connect(t)
	h.em.waitFor(t, "target-done", 1)
	dc, err := a.CheckDomain(" https://BAD.example/path ")
	if err != nil {
		t.Fatal(err)
	}
	if dc.OK || len(dc.Problems) != 2 {
		t.Fatalf("dc = %+v", dc)
	}
	for _, p := range dc.Problems {
		if strings.Contains(p, "_") || !strings.HasSuffix(p, ".") {
			t.Fatalf("problem not an English sentence: %q", p)
		}
	}
	if !strings.Contains(dc.Problems[1], "other.example") {
		t.Fatalf("redirect problem = %q", dc.Problems[1])
	}
	if strings.Join(dc.Alternatives, ",") != "b.example,d.example,a.example" {
		t.Fatalf("alternatives = %v", dc.Alternatives)
	}
	dc, err = a.CheckDomain("good.example")
	if err != nil || !dc.OK || len(dc.Alternatives) != 0 || len(dc.Problems) != 0 {
		t.Fatalf("good dc = %+v %v", dc, err)
	}
	if _, err := a.CheckDomain("not a domain"); err == nil {
		t.Fatal("want error for a bad domain")
	}
}

func TestAdoptAndReinstall(t *testing.T) {
	info := freshInfo()
	info.HasWayveil = true
	info.Port443Busy = true
	h := newHarness(t, info)
	a := h.start(t)
	if _, err := a.ServerConnect(AccessForm{Host: "1.2.3.4", User: "admin", Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	done, err := a.Adopt()
	if err != nil || !strings.HasPrefix(done.Link, "vless://") {
		t.Fatalf("Adopt = %+v, %v", done, err)
	}
	var kept []string
	for _, c := range h.callList() {
		if c != "suggest" {
			kept = append(kept, c)
		}
	}
	calls := strings.Join(kept, ",")
	if !strings.Contains(calls, "genkey,saveowner,adopt:admin:ssh-ed25519 AAAAappkey wayveil,addclient:Me,info") {
		t.Fatalf("calls = %s", calls)
	}
	if p, err := ownerstore.Load("1.2.3.4", 22); err != nil || p.User != "admin" || p.HostKey != "ssh-ed25519 AAAAfakehostkey" {
		t.Fatalf("owner = %+v %v", p, err)
	}

	h.mu.Lock()
	h.calls = nil
	h.mu.Unlock()
	done, err = a.Reinstall("www.microsoft.com")
	if err != nil || !strings.HasPrefix(done.Link, "vless://") {
		t.Fatalf("Reinstall = %+v, %v", done, err)
	}
	var order []string
	for _, s := range h.callList() {
		if s != "suggest" && s != "precheck" {
			order = append(order, s)
		}
	}
	if strings.Join(order, ",") != "uninstall,genkey,saveowner,install:,addclient:Me,info" {
		t.Fatalf("reinstall calls = %v", order)
	}
	var ids []string
	for _, e := range h.em.all() {
		if e.name == "step" {
			ids = append(ids, e.data.(StepEvent).ID)
		}
	}
	if len(ids) < 3 || ids[0] != "uninstall-service" || ids[1] != "uninstall-service" || ids[2] != "authkey" {
		t.Fatalf("step ids = %v", ids)
	}
}

func TestAdoptNeedsWayveil(t *testing.T) {
	h := newHarness(t, freshInfo())
	a := h.start(t)
	h.connect(t)
	if _, err := a.Adopt(); err == nil {
		t.Fatal("Adopt on a fresh server must fail")
	}
}

func TestReinstallRefusesForeign(t *testing.T) {
	info := freshInfo()
	info.HasForeignXray = true
	h := newHarness(t, info)
	a := h.start(t)
	h.connect(t)
	if _, err := a.Reinstall("www.microsoft.com"); !errors.Is(err, server.ErrForeignXray) {
		t.Fatalf("err = %v", err)
	}
	for _, c := range h.callList() {
		if c == "uninstall" || strings.HasPrefix(c, "install") {
			t.Fatalf("touched a foreign server: %v", h.callList())
		}
	}
}

// After a failed install the session's precheck is stale ("fresh"); the
// "Clean up and install again" button must still work.
func TestReinstallAfterFailedInstall(t *testing.T) {
	info := freshInfo()
	h := newHarness(t, info)
	fail := true
	inner := h.d.Install
	h.d.Install = func(ctx context.Context, r remote.Runner, o server.InstallOptions, on func(server.StepEvent)) (server.InstallResult, error) {
		if fail {
			fail = false
			return server.InstallResult{}, io.ErrUnexpectedEOF
		}
		return inner(ctx, r, o, on)
	}
	a := h.start(t)
	h.connect(t)
	if _, err := a.Install("www.microsoft.com"); err == nil {
		t.Fatal("want failure")
	}
	if _, err := a.Reinstall("www.microsoft.com"); err != nil {
		t.Fatal(err)
	}
}

func TestNotConnected(t *testing.T) {
	h := newHarness(t, freshInfo())
	a := h.start(t)
	if _, err := a.Install("www.microsoft.com"); err == nil {
		t.Fatal("want error without a session")
	}
	if _, err := a.CheckDomain("www.microsoft.com"); err == nil {
		t.Fatal("want error without a session")
	}
}
