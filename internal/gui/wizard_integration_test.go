//go:build integration

package gui

import (
	"context"
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

const itPassword = "wayveil"

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

func form(e *testenv.Env) AccessForm {
	return AccessForm{Host: e.Host, Port: e.Port, User: "root", Password: itPassword}
}

// noSecret fails if the password shows up in events or under cfg.
func noSecret(t *testing.T, rec *recorder, cfg string) {
	t.Helper()
	for _, e := range rec.snapshot() {
		if strings.Contains(fmt.Sprintf("%+v", e.data), itPassword) {
			t.Errorf("password in %s event: %+v", e.name, e.data)
		}
	}
	filepath.Walk(cfg, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), `"`+itPassword+`"`) { // the bare word is also the key comment
				t.Errorf("password in file %s", p)
			}
		}
		return nil
	})
}

func noSecretErr(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), itPassword) {
		t.Errorf("password in error: %v", err)
	}
}

func TestWizardFreshInstall(t *testing.T) {
	e := testenv.Start(t)
	a, rec, cfg := newApp(t, nil)

	hc, err := a.ServerConnect(form(e))
	if err != nil {
		t.Fatal(err)
	}
	if hc.State != "fresh" || hc.Fingerprint == "" || hc.Pinned {
		t.Fatalf("hostcheck %+v", hc)
	}
	rec.waitFor(t, 60*time.Second, "target", "target-done") // zero targets is fine

	done, err := a.Install("www.microsoft.com")
	noSecretErr(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(done.Link, "vless://") || !strings.HasPrefix(done.QR, "data:image/png;base64,") {
		t.Fatalf("done %+v", done)
	}
	root := e.Root(t)
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
	noSecret(t, rec, cfg)
}

func TestWizardTakeOver(t *testing.T) {
	e := testenv.Start(t)
	a, _, _ := newApp(t, nil)
	if _, err := a.ServerConnect(form(e)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Install("www.microsoft.com"); err != nil {
		t.Fatal(err)
	}
	a.Shutdown()

	b, rec, cfg := newApp(t, nil) // fresh dirs: no saved owner profile
	hc, err := b.ServerConnect(form(e))
	if err != nil {
		t.Fatal(err)
	}
	if hc.State != "wayveil" || hc.Pinned {
		t.Fatalf("hostcheck %+v", hc)
	}
	done, err := b.Adopt()
	noSecretErr(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(done.Link, "vless://") {
		t.Fatalf("done %+v", done)
	}
	list, err := server.ListClients(context.Background(), e.Root(t))
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
	noSecret(t, rec, cfg)
}

func TestWizardReinstallAfterFailure(t *testing.T) {
	e := testenv.Start(t)
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
	if _, err := a.ServerConnect(form(e)); err != nil {
		t.Fatal(err)
	}
	_, err := a.Install("www.microsoft.com")
	if err == nil || err.Error() == "" {
		t.Fatalf("want an error with a human message, got %v", err)
	}
	noSecretErr(t, err)
	failing = false

	hc, err := a.ServerConnect(form(e))
	if err != nil {
		t.Fatal(err)
	}
	if hc.State != "partial" {
		t.Fatalf("state %q (%+v)", hc.State, hc)
	}
	if _, err := a.Reinstall("www.microsoft.com"); err != nil {
		t.Fatal(err)
	}
	if s := testenv.Sh(t, e.Root(t), "systemctl is-active xray"); s != "active" {
		t.Fatalf("xray %s", s)
	}
	noSecret(t, rec, cfg)
}
