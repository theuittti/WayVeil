//go:build integration

package server

import (
	"context"
	"strings"
	"testing"

	"github.com/theuittti/WayVeil/internal/testenv"
)

func TestPrecheckFresh(t *testing.T) {
	c := testenv.Start(t).Root(t)
	h, err := Precheck(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if h.OSID != "ubuntu" || h.OSVersion != "24.04" || !h.Supported || h.Port443Busy || h.HasWayveil || h.HasForeignXray || h.Arch != "amd64" {
		t.Fatalf("%+v", h)
	}
}

func TestPrecheckPortBusy(t *testing.T) {
	c := testenv.Start(t).Root(t)
	testenv.Sh(t, c, "nohup python3 -m http.server 443 >/dev/null 2>&1 & sleep 1")
	h, err := Precheck(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if !h.Port443Busy || !strings.Contains(h.Port443Process, "python3") {
		t.Fatalf("%+v", h)
	}
}
