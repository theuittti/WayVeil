package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/theuittti/WayVeil/internal/remote"
)

// XrayVersion is the pinned xray-core release installed on servers.
const XrayVersion = "v26.10.31"

// Official SHA2-256 digests (Xray-linux-*.zip.dgst of XrayVersion); the
// installer refuses a download that does not match.
const (
	xraySHA256amd64 = "e4bd9119f50ae7c41658b1abd0679ae06f98bd864a2907ff33f2f2cf7081b214"
	xraySHA256arm64 = "1811c8f1da0c3b6dc707ebaf1c8390e987ba39859a8f0a7aadd8526157f2ec8d"
)

type InstallOptions struct {
	Address     string // what clients connect to (IP or domain)
	SNI         string // REALITY target
	AuthUser    string // SSH user whose authorized_keys gets AuthKeyLine
	AuthKeyLine string // the app's own public key line
	DNS         string // default "1.1.1.1"
	// XrayZip, if set, is the official Xray-linux-<arch>.zip of XrayVersion,
	// uploaded so the server needn't reach GitHub; its digest is still checked.
	XrayZip []byte
}

const uploadedZip = "/tmp/wayveil-xray.zip"

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
	ErrPartialInstall   = &ScriptError{Code: "partial_install"}
)

// Install prechecks the server, refuses before changing anything when it is
// not suitable, then runs the install script.
func Install(ctx context.Context, r remote.Runner, o InstallOptions, onStep func(StepEvent)) (InstallResult, error) {
	return installWith(ctx, r, o, onStep, nil)
}

// installWith is Install with extra script env (test hooks such as WV_FAIL_AT).
func installWith(ctx context.Context, r remote.Runner, o InstallOptions, onStep func(StepEvent), extra map[string]string) (InstallResult, error) {
	var res InstallResult
	h, err := Precheck(ctx, r)
	if err != nil {
		return res, err
	}
	switch {
	case !h.Supported:
		return res, &ScriptError{Code: ErrUnsupportedOS.Code, Message: h.OSID + " " + h.OSVersion}
	case h.PartialInstall:
		return res, ErrPartialInstall
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
		"WV_XRAY_SHA256_64": xraySHA256amd64, "WV_XRAY_SHA256_ARM64": xraySHA256arm64,
		"WV_UNINSTALLER": base64.StdEncoding.EncodeToString(script("uninstall")),
	}
	if len(o.XrayZip) > 0 {
		if _, err := r.Run(ctx, "install -m 0600 /dev/stdin "+uploadedZip, o.XrayZip); err != nil {
			return res, err
		}
		env["WV_XRAY_ZIP"] = uploadedZip
	}
	for k, v := range extra {
		env[k] = v
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
	if err := json.Unmarshal(out, &res); err != nil {
		return res, err
	}
	return res, nil
}
