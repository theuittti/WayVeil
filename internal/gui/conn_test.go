package gui

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/theuittti/WayVeil/internal/client"
	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/engine"
	"github.com/theuittti/WayVeil/internal/lists"
)

const testLink = "vless://11111111-2222-3333-4444-555555555555@1.2.3.4:443?security=reality&sni=www.example.com&pbk=abc&sid=01&fp=chrome&type=tcp&flow=xtls-rprx-vision#Home"

type connEngine struct {
	mu       sync.Mutex
	state    engine.State
	since    time.Time
	errStr   string
	granted  bool
	connects int
	block    chan struct{} // if set, Connect waits for it (or ctx)
	entered  chan struct{}
}

func (e *connEngine) Connect(ctx context.Context, _ clientstore.Settings) error {
	e.mu.Lock()
	e.connects++
	g := e.granted
	e.mu.Unlock()
	if !g {
		return engine.ErrNeedsElevation
	}
	if e.block != nil {
		e.entered <- struct{}{}
		select {
		case <-e.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	e.mu.Lock()
	e.state = engine.Connected
	e.mu.Unlock()
	return nil
}
func (e *connEngine) Disconnect(context.Context) error {
	e.mu.Lock()
	e.state = engine.Disconnected
	e.mu.Unlock()
	return nil
}
func (e *connEngine) Status(context.Context) engine.Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return engine.Status{State: e.state, Since: e.since, Error: e.errStr, Up: 5, Down: 7}
}

func connApp(t *testing.T, e *connEngine, grants *int) *App {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	d := DefaultDeps(t.TempDir())
	d.Client = client.Deps{
		Engine: e,
		Grant: func(context.Context, string) error {
			*grants++
			e.mu.Lock()
			e.granted = true
			e.mu.Unlock()
			return nil
		},
		Update: func(context.Context, lists.Options) (lists.UpdateResult, error) {
			return lists.UpdateResult{Updated: []string{"x"}}, nil
		},
		Mihomo: func() (string, error) { return "/x/mihomo", nil },
	}
	d.ClientErr = nil
	a := New(&recEmitter{}, d)
	t.Cleanup(a.Shutdown)
	return a
}

func TestImportLinkTrims(t *testing.T) {
	a := connApp(t, &connEngine{}, new(int))
	st, err := a.ImportLink("  " + testLink + "\n")
	if err != nil || !st.HasProfile || st.ProfileName != "Home" || st.Server != "1.2.3.4" || st.Mode != "smart" {
		t.Fatalf("%+v %v", st, err)
	}
	if !a.State().HasProfile {
		t.Fatal("not persisted")
	}
}

func TestImportLinkRejects(t *testing.T) {
	a := connApp(t, &connEngine{}, new(int))
	for _, in := range []string{"https://x", ""} {
		_, err := a.ImportLink(in)
		if err == nil || err.Error() != "This is not a vless:// connection link." {
			t.Fatalf("%q: %v", in, err)
		}
	}
	if _, err := a.ImportLink("vless://broken"); err == nil || err.Error() != "This connection link is incomplete or damaged." {
		t.Fatal(err)
	}
	if st, err := a.ImportQRFile(""); err != nil || st.HasProfile {
		t.Fatal(st, err)
	}
}

func TestConnectBusy(t *testing.T) {
	e := &connEngine{granted: true, block: make(chan struct{}), entered: make(chan struct{}, 2)}
	a := connApp(t, e, new(int))
	if _, err := a.ImportLink(testLink); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := a.Connect(); done <- err }()
	<-e.entered
	if _, err := a.Connect(); !errors.Is(err, ErrBusy) {
		t.Fatalf("got %v", err)
	}
	close(e.block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConnectElevationOnce(t *testing.T) {
	e := &connEngine{}
	grants := 0
	a := connApp(t, e, &grants)
	if _, err := a.ImportLink(testLink); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		st, err := a.Connect()
		if err != nil || st.State != "connected" {
			t.Fatalf("%d: %+v %v", i, st, err)
		}
		if st, err := a.Disconnect(); err != nil || st.State != "disconnected" {
			t.Fatalf("%d: %+v %v", i, st, err)
		}
	}
	if grants != 1 {
		t.Fatalf("grants %d", grants)
	}
}

func TestConnectNoProfile(t *testing.T) {
	a := connApp(t, &connEngine{}, new(int))
	_, err := a.Connect()
	if err == nil || err.Error() != "No connection link yet. Paste one first." || !errors.Is(err, client.ErrNoProfile) {
		t.Fatal(err)
	}
}

func TestStatusMapping(t *testing.T) {
	since := time.Unix(1700000000, 0)
	e := &connEngine{state: engine.Connected, since: since, errStr: "boom"}
	a := connApp(t, e, new(int))
	st := a.Status()
	if st.State != "connected" || st.Since != since.UnixMilli() || st.Up != 5 || st.Down != 7 || st.Error != "Something went wrong: boom" {
		t.Fatalf("%+v", st)
	}
	e.state, e.errStr = engine.Disconnected, ""
	if st := a.Status(); st.Since != 0 || st.Error != "" {
		t.Fatalf("%+v", st)
	}
}

func TestStatusEngineUnavailable(t *testing.T) {
	a := connApp(t, &connEngine{}, new(int))
	a.d.Client.Engine = nil
	a.d.ClientErr = errors.New("no cache dir")
	if st := a.Status(); st.State != "failed" || st.Error == "" {
		t.Fatalf("%+v", st)
	}
	if _, err := a.Connect(); err == nil {
		t.Fatal("want error")
	}
}

func TestSetModeValidates(t *testing.T) {
	a := connApp(t, &connEngine{}, new(int))
	if _, err := a.SetMode("bogus"); err == nil {
		t.Fatal("want error")
	}
	if _, err := a.SetMode("all"); err != nil {
		t.Fatal(err)
	}
	if a.State().Mode != "all" {
		t.Fatal("mode not saved")
	}
}
