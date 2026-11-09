// Package client holds the connect/apply/lists logic shared by the CLI and
// the GUI backend.
package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/engine"
	"github.com/theuittti/WayVeil/internal/lists"
)

// Engine is the part of the VPN engine the client logic needs.
type Engine interface {
	Connect(context.Context, clientstore.Settings) error
	Disconnect(context.Context) error
	Status(context.Context) engine.Status
}

// Deps are the injectable collaborators.
type Deps struct {
	Engine Engine
	Grant  func(ctx context.Context, bin string) error                      // engine.GrantCaps
	Update func(context.Context, lists.Options) (lists.UpdateResult, error) // lists.Update
	Mihomo func() (string, error)                                           // mihomo.Find
	Warn   func(msg string)                                                 // may be nil
	// BeforeGrant, if set, is called just before the one-time permission
	// prompt (so callers can explain it). May be nil.
	BeforeGrant func()
}

// ListsMaxAge is how old the block lists may get before Connect refreshes them.
const ListsMaxAge = 24 * time.Hour

// ErrNoProfile means no connection link has been imported yet.
var ErrNoProfile = errors.New("no_profile")

func (d Deps) warn(msg string) {
	if d.Warn != nil {
		d.Warn(msg)
	}
}

// Connect refreshes stale lists (failure only warns) and connects. If the
// engine needs elevation, Grant is attempted once and Connect retried once.
func Connect(ctx context.Context, s *clientstore.Settings, d Deps) error {
	if s.Profile == nil {
		return ErrNoProfile
	}
	if time.Since(s.ListsUpdated) > ListsMaxAge {
		if _, err := UpdateLists(ctx, s, d); err != nil {
			d.warn("could not refresh block lists, using saved ones: " + err.Error())
		}
	}
	err := d.Engine.Connect(ctx, *s)
	if errors.Is(err, engine.ErrNeedsElevation) {
		if d.BeforeGrant != nil {
			d.BeforeGrant()
		}
		bin, ferr := d.Mihomo()
		if ferr != nil {
			return ferr
		}
		if gerr := d.Grant(ctx, bin); gerr != nil {
			return fmt.Errorf("permission for the TUN device was not granted: %w", gerr)
		}
		err = d.Engine.Connect(ctx, *s)
	}
	return err
}

// SaveAndApply saves settings and reconnects if a connection is running.
func SaveAndApply(ctx context.Context, s *clientstore.Settings, d Deps) (reconnected bool, err error) {
	if err := clientstore.Save(*s); err != nil {
		return false, err
	}
	if st := d.Engine.Status(ctx).State; st == engine.Connected || st == engine.Connecting {
		if err := d.Engine.Disconnect(ctx); err != nil {
			return false, err
		}
		if err := Connect(ctx, s, d); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// UpdateLists downloads the block lists and records the time. The result is
// returned even on error so callers can report per-list details.
func UpdateLists(ctx context.Context, s *clientstore.Settings, d Deps) (lists.UpdateResult, error) {
	dir, err := lists.DefaultDir()
	if err != nil {
		return lists.UpdateResult{}, err
	}
	res, err := d.Update(ctx, lists.Options{Dir: dir})
	if err != nil {
		return res, err
	}
	if len(res.Updated) == 0 {
		return res, errors.New("no list could be downloaded")
	}
	s.ListsUpdated = time.Now()
	return res, clientstore.Save(*s)
}
