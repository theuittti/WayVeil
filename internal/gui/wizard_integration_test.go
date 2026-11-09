//go:build integration

package gui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/theuittti/WayVeil/internal/ownerstore"
	"github.com/theuittti/WayVeil/internal/remote"
	"github.com/theuittti/WayVeil/internal/server"
	"github.com/theuittti/WayVeil/internal/testenv"
)

type installFn = func(context.Context, remote.Runner, server.InstallOptions, func(server.StepEvent)) (server.InstallResult, error)

type recEvent struct {
	name string
	data any
}

// recorder is a goroutine-safe Emitter.
type recorder struct {
	mu sync.Mutex
	ev []recEvent
}

func (r *recorder) Emit(name string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ev = append(r.ev, recEvent{name, data})
}

func (r *recorder) snapshot() []recEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recEvent(nil), r.ev...)
}

func (r *recorder) has(name string) bool {
	for _, e := range r.snapshot() {
		if e.name == name {
			return true
		}
	}
	return false
}

func (r *recorder) waitFor(t *testing.T, d time.Duration, names ...string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, n := range names {
			if r.has(n) {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no %v event within %s", names, d)
}

// newApp builds an App with real deps, fresh config/cache dirs and the local
// Xray zip. wrap, if set, replaces Deps.Install.
func newApp(t *testing.T, wrap func(real installFn) installFn) (*App, *recorder, string) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	d := DefaultDeps(filepath.Join("..", "..", "build"))
	d.XrayZip = func(arch string) []byte {
		if arch == "amd64" {
			return testenv.XrayZip()
		}
		return nil
	}
	if wrap != nil {
		d.Install = wrap(d.Install)
	}
	rec := &recorder{}
	a := New(rec, d)
	t.Cleanup(a.Shutdown)
	return a, rec, cfg
}

// strongPassword gives the container's root a unique random password (so a
// leak check by plain substring cannot false-positive) and returns it.
func strongPassword(t *testing.T, e *testenv.Env) string {
	t.Helper()
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	pw := "Pw" + hex.EncodeToString(b)
	testenv.Sh(t, e.Root(t), "echo 'root:"+pw+"' | chpasswd")
	return pw
}

func form(e *testenv.Env, pw string) AccessForm {
	return AccessForm{Host: e.Host, Port: e.Port, User: "root", Password: pw}
}

// leaks lists every place the password shows up: recorded events (JSON and
// %+v) and every file under cfg. It also fails the test if nothing was scanned.
func leaks(t *testing.T, pw string, rec *recorder, cfg string) []string {
	t.Helper()
	var out []string
	for _, e := range rec.snapshot() {
		j, err := json.Marshal(e.data)
		if err != nil {
			t.Fatalf("marshal %s event: %v", e.name, err)
		}
		if strings.Contains(string(j), pw) || strings.Contains(fmt.Sprintf("%+v", e.data), pw) {
			out = append(out, "event "+e.name)
		}
	}
	files := 0
	err := filepath.Walk(cfg, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files++
		if strings.Contains(string(b), pw) {
			out = append(out, "file "+p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", cfg, err)
	}
	if files == 0 {
		t.Fatalf("no files under %s were scanned", cfg)
	}
	return out
}

func noSecret(t *testing.T, pw string, rec *recorder, cfg string) {
	t.Helper()
	for _, l := range leaks(t, pw, rec, cfg) {
		t.Errorf("password leaked in %s", l)
	}
}

func noSecretErr(t *testing.T, pw string, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), pw) {
		t.Errorf("password in error: %v", err)
	}
}

// The checker itself must notice a leak (positive control).
func TestLeakCheckerPositiveControl(t *testing.T) {
	cfg := t.TempDir()
	rec := &recorder{}
	os.WriteFile(filepath.Join(cfg, "a.txt"), []byte("clean"), 0o600)
	if got := leaks(t, "sekrit-pw", rec, cfg); len(got) != 0 {
		t.Fatalf("false positive: %v", got)
	}
	os.WriteFile(filepath.Join(cfg, "b.txt"), []byte("xx sekrit-pw yy"), 0o600)
	rec.Emit("step", StepEvent{Message: "oops sekrit-pw"})
	if got := leaks(t, "sekrit-pw", rec, cfg); len(got) != 2 {
		t.Fatalf("leaks not detected: %v", got)
	}
}

func TestWizardFreshInstall(t *testing.T) {
	e := testenv.Start(t)
	pw := strongPassword(t, e)
	a, rec, cfg := newApp(t, nil)

	hc, err := a.ServerConnect(form(e, pw))
	if err != nil {
		t.Fatal(err)
	}
	if hc.State != "fresh" || hc.Fingerprint == "" || hc.Pinned {
		t.Fatalf("hostcheck %+v", hc)
	}
	rec.waitFor(t, 60*time.Second, "target", "target-done") // zero targets is fine

	done, err := a.Install("www.microsoft.com")
	noSecretErr(t, pw, err)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(done.Link, "vless://") || !strings.HasPrefix(done.QR, "data:image/png;base64,") {
		t.Fatalf("done %+v", done)
	}
	root := e.Connect(t, "root", pw)
	if s := testenv.Sh(t, root, "systemctl is-active xray"); s != "active" {
		t.Fatalf("xray %s", s)
	}
	p, err := ownerstore.Load(e.Host, e.Port)
	if err != nil || len(p.PrivateKeyPEM) == 0 || p.HostKey == "" {
		t.Fatalf("owner profile %+v %v", p, err)
	}
	verified := false
	for _, ev := range rec.snapshot() {
		if s, ok := ev.data.(StepEvent); ok && ev.name == "step" && s.ID == "verify" && s.Status == "ok" {
			verified = true
		}
	}
	if !verified {
		t.Fatal("no verify ok step event")
	}
	noSecret(t, pw, rec, cfg)
}

func TestWizardTakeOver(t *testing.T) {
	e := testenv.Start(t)
	pw := strongPassword(t, e)
	a, recA, cfgA := newApp(t, nil)
	if _, err := a.ServerConnect(form(e, pw)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Install("www.microsoft.com"); err != nil {
		t.Fatal(err)
	}
	a.Shutdown()
	noSecret(t, pw, recA, cfgA)

	b, rec, cfg := newApp(t, nil) // fresh dirs: no saved owner profile
	hc, err := b.ServerConnect(form(e, pw))
	if err != nil {
		t.Fatal(err)
	}
	if hc.State != "wayveil" || hc.Pinned {
		t.Fatalf("hostcheck %+v", hc)
	}
	done, err := b.Adopt()
	noSecretErr(t, pw, err)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(done.Link, "vless://") {
		t.Fatalf("done %+v", done)
	}
	list, err := server.ListClients(context.Background(), e.Connect(t, "root", pw))
	if err != nil {
		t.Fatal(err)
	}
	me := 0
	for _, c := range list {
		if c.Name == "Me" {
			me++
		}
	}
	if me != 2 {
		t.Fatalf("want two Me clients, got %+v", list)
	}
	noSecret(t, pw, rec, cfg)
}

func TestWizardReinstallAfterFailure(t *testing.T) {
	e := testenv.Start(t)
	pw := strongPassword(t, e)
	failing := true
	a, rec, cfg := newApp(t, func(real installFn) installFn {
		return func(ctx context.Context, r remote.Runner, o server.InstallOptions, on func(server.StepEvent)) (server.InstallResult, error) {
			if !failing {
				return real(ctx, r, o, on)
			}
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			return real(ctx, r, o, func(ev server.StepEvent) {
				if ev.ID == "config" && ev.Status == server.StepStart {
					cancel() // simulates a dropped connection
				}
				on(ev)
			})
		}
	})
	if _, err := a.ServerConnect(form(e, pw)); err != nil {
		t.Fatal(err)
	}
	_, err := a.Install("www.microsoft.com")
	var ue *UIError
	if !errors.As(err, &ue) || ue.Error() == "" {
		t.Fatalf("want a UIError with a human message, got %T %v", err, err)
	}
	noSecretErr(t, pw, err)
	failing = false

	hc, err := a.ServerConnect(form(e, pw))
	if err != nil {
		t.Fatal(err)
	}
	if hc.State != "partial" {
		t.Fatalf("state %q (%+v)", hc.State, hc)
	}
	if _, err := a.Reinstall("www.microsoft.com"); err != nil {
		t.Fatal(err)
	}
	if s := testenv.Sh(t, e.Connect(t, "root", pw), "systemctl is-active xray"); s != "active" {
		t.Fatalf("xray %s", s)
	}
	noSecret(t, pw, rec, cfg)
}
