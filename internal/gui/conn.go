package gui

import (
	"context"
	"errors"
	"strings"

	"github.com/theuittti/WayVeil/internal/client"
	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/engine"
	"github.com/theuittti/WayVeil/internal/link"
	"github.com/theuittti/WayVeil/internal/profile"
)

// UIState is what the first screens need to decide what to show.
type UIState struct {
	HasProfile  bool   `json:"hasProfile"`
	ProfileName string `json:"profileName"`
	Server      string `json:"server"`
	HasOwner    bool   `json:"hasOwner"` // an owner server is saved
	Mode        string `json:"mode"`     // "smart" | "all"
}

// ConnStatus is the VPN connection state for the UI.
type ConnStatus struct {
	State string `json:"state"` // "disconnected" | "connecting" | "connected" | "failed"
	Since int64  `json:"since"` // unix ms, 0 when not connected
	Up    int64  `json:"up"`
	Down  int64  `json:"down"`
	Error string `json:"error"` // Human() text
}

var (
	errNotVless = &UIError{msg: "This is not a vless:// connection link.", err: link.ErrNotVless}
	errBadLink  = &UIError{msg: "This connection link is incomplete or damaged."}
	errNoQR     = &UIError{msg: "No QR code found in this image.", err: profile.ErrNoQR}
	errBadImage = &UIError{msg: "Could not read this image."}
	errBadMode  = &UIError{msg: "Unknown mode."}
	errNoEngine = errors.New("engine unavailable")
)

func (a *App) state(s clientstore.Settings) UIState {
	st := UIState{Mode: string(s.Mode)}
	if s.Mode != clientstore.ModeAll {
		st.Mode = string(clientstore.ModeSmart)
	}
	if s.Profile != nil {
		st.HasProfile = true
		st.ProfileName = s.Profile.Name
		st.Server = s.Profile.Params.Host
	}
	st.HasOwner = a.d.HasOwner != nil && a.d.HasOwner()
	return st
}

// State is the saved profile/mode summary.
func (a *App) State() UIState {
	s, _ := a.d.LoadSettings()
	return a.state(s)
}

func (a *App) saveProfile(p profile.Profile) (UIState, error) {
	s, err := a.d.LoadSettings()
	if err != nil {
		return a.state(s), uiError(err)
	}
	s.Profile = &p
	if err := a.d.SaveSettings(s); err != nil {
		return a.state(s), uiError(err)
	}
	return a.state(s), nil
}

// ImportLink saves a pasted vless:// link as the connection profile.
func (a *App) ImportLink(s string) (UIState, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(strings.ToLower(s), "vless://") {
		return a.State(), errNotVless
	}
	p, err := profile.FromLink(s)
	if err != nil {
		if errors.Is(err, link.ErrNotVless) {
			return a.State(), errNotVless
		}
		return a.State(), errBadLink
	}
	return a.saveProfile(p)
}

// ImportQRFile reads a QR image; "" means the file dialog was cancelled.
func (a *App) ImportQRFile(path string) (UIState, error) {
	if path == "" {
		return a.State(), nil
	}
	p, err := profile.FromQRImage(path)
	if err != nil {
		switch {
		case errors.Is(err, profile.ErrNoQR):
			return a.State(), errNoQR
		case errors.Is(err, link.ErrNotVless):
			return a.State(), errNotVless
		}
		var mf link.ErrMissingField
		if errors.As(err, &mf) {
			return a.State(), errBadLink
		}
		return a.State(), errBadImage
	}
	return a.saveProfile(p)
}

// beginConn takes the connection busy flag; end releases it.
func (a *App) beginConn() (context.Context, func(), error) {
	a.mu.Lock()
	if a.connBusy {
		a.mu.Unlock()
		return nil, nil, ErrBusy
	}
	if err := a.ctx.Err(); err != nil {
		a.mu.Unlock()
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.connBusy, a.connCancel = true, cancel
	a.mu.Unlock()
	a.connOp.Lock()
	return ctx, func() {
		cancel()
		a.connOp.Unlock()
		a.mu.Lock()
		a.connBusy, a.connCancel = false, nil
		a.mu.Unlock()
	}, nil
}

func (a *App) engineErr() error {
	if a.d.Client.Engine != nil {
		return nil
	}
	if a.d.ClientErr != nil {
		return a.d.ClientErr
	}
	return errNoEngine
}

// Connect starts the VPN (asks for permission once if needed).
func (a *App) Connect() (ConnStatus, error) {
	ctx, end, err := a.beginConn()
	if err != nil {
		return a.Status(), uiError(err)
	}
	defer end()
	if err := a.engineErr(); err != nil {
		return a.Status(), uiError(err)
	}
	s, err := a.d.LoadSettings()
	if err != nil {
		return a.Status(), uiError(err)
	}
	if err := client.Connect(ctx, &s, a.d.Client); err != nil {
		return a.Status(), uiError(err)
	}
	return a.Status(), nil
}

// Disconnect stops the VPN, cancelling a connect in progress.
func (a *App) Disconnect() (ConnStatus, error) {
	a.mu.Lock()
	if a.connCancel != nil {
		a.connCancel()
	}
	a.mu.Unlock()
	a.connOp.Lock()
	defer a.connOp.Unlock()
	if err := a.engineErr(); err != nil {
		return a.Status(), uiError(err)
	}
	if err := a.d.Client.Engine.Disconnect(a.ctx); err != nil {
		return a.Status(), uiError(err)
	}
	return a.Status(), nil
}

// Status is the current connection state.
func (a *App) Status() ConnStatus {
	if err := a.engineErr(); err != nil {
		return ConnStatus{State: string(engine.Failed), Error: Human(err)}
	}
	st := a.d.Client.Engine.Status(a.ctx)
	cs := ConnStatus{State: string(st.State), Up: st.Up, Down: st.Down}
	if !st.Since.IsZero() && st.State == engine.Connected {
		cs.Since = st.Since.UnixMilli()
	}
	if st.Error != "" {
		cs.Error = Human(errors.New(st.Error))
	}
	return cs
}

// SetMode saves the routing mode and reconnects if a connection is running.
func (a *App) SetMode(mode string) (ConnStatus, error) {
	m := clientstore.Mode(mode)
	if m != clientstore.ModeSmart && m != clientstore.ModeAll {
		return a.Status(), errBadMode
	}
	ctx, end, err := a.beginConn()
	if err != nil {
		return a.Status(), uiError(err)
	}
	defer end()
	if err := a.engineErr(); err != nil {
		return a.Status(), uiError(err)
	}
	s, err := a.d.LoadSettings()
	if err != nil {
		return a.Status(), uiError(err)
	}
	s.Mode = m
	if _, err := client.SaveAndApply(ctx, &s, a.d.Client); err != nil {
		return a.Status(), uiError(err)
	}
	return a.Status(), nil
}
