package client

import (
	"context"
	"errors"
	"testing"

	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/engine"
	"github.com/theuittti/WayVeil/internal/lists"
	"github.com/theuittti/WayVeil/internal/profile"
)

type fakeEngine struct {
	state       engine.State
	connectErrs []error
	connects    int
	disconnects int
}

func (f *fakeEngine) Connect(context.Context, clientstore.Settings) error {
	f.connects++
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
	return engine.Status{State: f.state}
}

type rig struct {
	eng       *fakeEngine
	grants    int
	grantErr  error
	updateErr error
	warns     []string
	d         Deps
	s         *clientstore.Settings
}

func newRig(t *testing.T, withProfile bool) *rig {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	r := &rig{eng: &fakeEngine{}, s: &clientstore.Settings{}}
	if withProfile {
		r.s.Profile = &profile.Profile{Name: "n"}
	}
	r.d = Deps{
		Engine: r.eng,
		Grant:  func(context.Context, string) error { r.grants++; return r.grantErr },
		Update: func(context.Context, lists.Options) (lists.UpdateResult, error) {
			if r.updateErr != nil {
				return lists.UpdateResult{}, r.updateErr
			}
			return lists.UpdateResult{Updated: []string{"ru-blocked"}}, nil
		},
		Mihomo: func() (string, error) { return "/x/mihomo", nil },
		Warn:   func(m string) { r.warns = append(r.warns, m) },
	}
	return r
}

func TestConnectGrantsOnceOnElevation(t *testing.T) {
	r := newRig(t, true)
	r.eng.connectErrs = []error{engine.ErrNeedsElevation, nil}
	if err := Connect(context.Background(), r.s, r.d); err != nil {
		t.Fatal(err)
	}
	if r.grants != 1 || r.eng.connects != 2 {
		t.Fatalf("grants %d connects %d", r.grants, r.eng.connects)
	}
}

func TestConnectElevationDeniedNoLoop(t *testing.T) {
	r := newRig(t, true)
	r.eng.connectErrs = []error{engine.ErrNeedsElevation}
	r.grantErr = engine.ErrElevationDenied
	err := Connect(context.Background(), r.s, r.d)
	if !errors.Is(err, engine.ErrElevationDenied) || r.eng.connects != 1 {
		t.Fatalf("err %v connects %d", err, r.eng.connects)
	}
}

func TestConnectElevationStillNeededNoLoop(t *testing.T) {
	r := newRig(t, true)
	r.eng.connectErrs = []error{engine.ErrNeedsElevation, engine.ErrNeedsElevation, nil}
	err := Connect(context.Background(), r.s, r.d)
	if !errors.Is(err, engine.ErrNeedsElevation) || r.grants != 1 || r.eng.connects != 2 {
		t.Fatalf("err %v grants %d connects %d", err, r.grants, r.eng.connects)
	}
}

func TestConnectNoProfile(t *testing.T) {
	r := newRig(t, false)
	if err := Connect(context.Background(), r.s, r.d); !errors.Is(err, ErrNoProfile) {
		t.Fatal(err)
	}
}

func TestConnectStaleListsWarnsAndContinues(t *testing.T) {
	r := newRig(t, true)
	r.updateErr = errors.New("offline")
	if err := Connect(context.Background(), r.s, r.d); err != nil {
		t.Fatal(err)
	}
	if len(r.warns) != 1 || r.eng.connects != 1 {
		t.Fatalf("warns %v connects %d", r.warns, r.eng.connects)
	}
}

func TestSaveAndApplyReconnectsOnlyWhenConnected(t *testing.T) {
	r := newRig(t, true)
	re, err := SaveAndApply(context.Background(), r.s, r.d)
	if err != nil || re || r.eng.connects != 0 {
		t.Fatalf("re %v err %v connects %d", re, err, r.eng.connects)
	}
	r.eng.state = engine.Connected
	re, err = SaveAndApply(context.Background(), r.s, r.d)
	if err != nil || !re || r.eng.disconnects != 1 || r.eng.connects != 1 {
		t.Fatalf("re %v err %v d %d c %d", re, err, r.eng.disconnects, r.eng.connects)
	}
}
