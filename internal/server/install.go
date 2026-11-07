package server

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/wayveil/wayveil/internal/remote"
)

// XrayVersion is the pinned xray-core release installed on servers.
const XrayVersion = "v26.10.31"

type InstallOptions struct {
	Address     string // what clients connect to (IP or domain)
	SNI         string // REALITY target
	AuthUser    string // SSH user whose authorized_keys gets AuthKeyLine
	AuthKeyLine string // the app's own public key line
	DNS         string // default "1.1.1.1"
}

type InstallResult struct {
	PublicKey   string      `json:"publicKey"`
	ShortID     string      `json:"shortId"`
	XrayVersion string      `json:"xrayVersion"`
	Warnings    []StepEvent `json:"-"`
}

var (
	ErrAlreadyInstalled = &ScriptError{Code: "already_installed"}
	ErrUnsupportedOS    = &ScriptError{Code: "unsupported_os"}
	ErrPortBusy         = &ScriptError{Code: "port_busy"}
	ErrForeignXray      = &ScriptError{Code: "foreign_xray"}
)

// Install prechecks the server, refuses before changing anything when it is
// not suitable, then runs the install script.
func Install(ctx context.Context, r remote.Runner, o InstallOptions, onStep func(StepEvent)) (InstallResult, error) {
	var res InstallResult
	h, err := Precheck(ctx, r)
	if err != nil {
		return res, err
	}
	switch {
	case !h.Supported:
		return res, &ScriptError{Code: ErrUnsupportedOS.Code, Message: h.OSID + " " + h.OSVersion}
	case h.HasWayveil:
		return res, ErrAlreadyInstalled
	case h.HasForeignXray:
		return res, ErrForeignXray
	case h.Port443Busy:
		return res, &ScriptError{Code: ErrPortBusy.Code, Message: h.Port443Process}
	}
	if o.DNS == "" {
		o.DNS = "1.1.1.1"
	}
	if o.Address == "" || o.SNI == "" || o.AuthUser == "" || o.AuthKeyLine == "" {
		return res, errors.New("install: Address, SNI, AuthUser and AuthKeyLine are required")
	}
	env := map[string]string{
		"WV_ADDRESS": o.Address, "WV_SNI": o.SNI, "WV_DNS": o.DNS, "WV_AUTH_USER": o.AuthUser,
		"WV_AUTH_KEY": o.AuthKeyLine, "WV_XRAY_VERSION": XrayVersion,
	}
	out, err := runScript(ctx, r, "install", env, func(e StepEvent) {
		if e.Status == StepWarn {
			res.Warnings = append(res.Warnings, e)
		}
		if onStep != nil {
			onStep(e)
		}
	}, nil)
	if err != nil {
		return res, err
	}
	if len(out) == 0 {
		return res, errors.New("install: no result")
	}
	err = json.Unmarshal(out, &res)
	return res, err
}
