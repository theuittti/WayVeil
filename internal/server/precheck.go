package server

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/wayveil/wayveil/internal/remote"
)

type HostInfo struct {
	OSID           string `json:"osId"`
	OSVersion      string `json:"osVersion"`
	Arch           string `json:"arch"` // "amd64" | "arm64"
	Supported      bool   `json:"supported"`
	Port443Busy    bool   `json:"port443Busy"`
	Port443Process string `json:"port443Process"`
	HasWayveil     bool   `json:"hasWayveil"`
	HasForeignXray bool   `json:"hasForeignXray"`
	PublicIP       string `json:"publicIp"`
}

// Precheck inspects the server without changing anything.
func Precheck(ctx context.Context, r remote.Runner) (HostInfo, error) {
	var h HostInfo
	res, err := runScript(ctx, r, "precheck", nil, nil, nil)
	if err != nil {
		return h, err
	}
	if len(res) == 0 {
		return h, errors.New("precheck: no result")
	}
	err = json.Unmarshal(res, &h)
	return h, err
}
