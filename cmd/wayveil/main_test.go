package main

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/engine"
	"github.com/theuittti/WayVeil/internal/link"
	"github.com/theuittti/WayVeil/internal/lists"
)

type fakeEngine struct {
	state       engine.State
	connectErrs []error
	connects    []clientstore.Settings
	disconnects int
}

func (f *fakeEngine) Connect(_ context.Context, s clientstore.Settings) error {
	f.connects = append(f.connects, s)
	if len(f.connectErrs) > 0 {
		err := f.connectErrs[0]
		f.connectErrs = f.connectErrs[1:]
		if err != nil {
			return err
		}
	}
	f.state = engine.Connected
	return nil
}
func (f *fakeEngine) Disconnect(context.Context) error {
	f.disconnects++
	f.state = engine.Disconnected
	return nil
}
func (f *fakeEngine) Status(context.Context) engine.Status {
	st := f.state
	if st == "" {
		st = engine.Disconnected
	}
	return engine.Status{State: st, Server: "srv", Mode: clientstore.ModeSmart, Since: time.Now()}
}

type harness struct {
	out      bytes.Buffer
	eng      *fakeEngine
	grants   int
	grantErr error
	d        deps
}

func newHarness(t *testing.T) *harness {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	h := &harness{eng: &fakeEngine{}}
	h.d = deps{
		out:    &h.out,
		engine: h.eng,
		grant:  func(context.Context, string) error { h.grants++; return h.grantErr },
		update: func(context.Context, lists.Options) (lists.UpdateResult, error) {
			return lists.UpdateResult{Updated: []string{"ru-blocked"}}, nil
		},
		singbox: func() (string, error) { return "/x/sing-box", nil },
	}
	return h
}

func (h *harness) run(args ...string) int { h.out.Reset(); return run(args, h.d) }

var vless = link.Build(link.Params{UUID: "u-1", Host: "203.0.113.10", SNI: "www.nu.nl", PublicKey: "PBK", ShortID: "ab12", Name: "Друг"})

func TestImportThenStatus(t *testing.T) {
	h := newHarness(t)
	if code := h.run("import", vless); code != 0 {
		t.Fatalf("import: %s", h.out.String())
	}
	s, _ := clientstore.Load()
	if s.Profile == nil || s.Profile.Name != "Друг" {
		t.Fatalf("%+v", s.Profile)
	}
	if h.run("status"); !strings.Contains(h.out.String(), "disconnected") {
		t.Fatal(h.out.String())
	}
}

func TestConnectGrantsOnce(t *testing.T) {
	h := newHarness(t)
	h.run("import", vless)
	h.eng.connectErrs = []error{engine.ErrNeedsElevation, nil}
	if code := h.run("connect"); code != 0 || h.grants != 1 || len(h.eng.connects) != 2 {
		t.Fatalf("code %d grants %d connects %d: %s", code, h.grants, len(h.eng.connects), h.out.String())
	}
	s, _ := clientstore.Load()
	if time.Since(s.ListsUpdated) > time.Minute {
		t.Fatal("lists should have been refreshed and the time saved")
	}
}

func TestConnectGrantDenied(t *testing.T) {
	h := newHarness(t)
	h.run("import", vless)
	h.eng.connectErrs = []error{engine.ErrNeedsElevation}
	h.grantErr = engine.ErrElevationDenied
	if code := h.run("connect"); code != 1 || !strings.Contains(h.out.String(), "TUN") {
		t.Fatalf("code %d: %s", code, h.out.String())
	}
}

func TestRulesNormalised(t *testing.T) {
	h := newHarness(t)
	if code := h.run("rules", "proxy", "https://Example.COM/x"); code != 0 {
		t.Fatal(h.out.String())
	}
	s, _ := clientstore.Load()
	if !reflect.DeepEqual(s.AlwaysProxy, []string{"example.com"}) {
		t.Fatalf("%v", s.AlwaysProxy)
	}
	if code := h.run("rules", "direct", "localhost"); code != 1 {
		t.Fatal("bad domain must fail")
	}
	h.run("rules", "rm", "example.com")
	if s, _ = clientstore.Load(); len(s.AlwaysProxy) != 0 {
		t.Fatalf("%v", s.AlwaysProxy)
	}
}

func TestModeReconnects(t *testing.T) {
	h := newHarness(t)
	h.run("import", vless)
	h.run("connect")
	if code := h.run("mode", "all"); code != 0 {
		t.Fatal(h.out.String())
	}
	last := h.eng.connects[len(h.eng.connects)-1]
	if h.eng.disconnects == 0 || last.Mode != clientstore.ModeAll {
		t.Fatalf("disconnects %d mode %s", h.eng.disconnects, last.Mode)
	}
	if code := h.run("mode", "fast"); code != 1 {
		t.Fatal("unknown mode must fail")
	}
}

func TestConnectWithoutProfile(t *testing.T) {
	h := newHarness(t)
	if code := h.run("connect"); code != 1 || !strings.Contains(h.out.String(), "wayveil import") {
		t.Fatalf("code %d: %s", code, h.out.String())
	}
}
