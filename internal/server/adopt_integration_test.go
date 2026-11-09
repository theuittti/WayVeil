//go:build integration

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/theuittti/WayVeil/internal/sshx"
	"github.com/theuittti/WayVeil/internal/testenv"
)

func TestAdoptKeyRemovedByUninstall(t *testing.T) {
	e := testenv.Start(t)
	root, _, _ := installed(t, e, "root", "wayveil")
	_, keyB, _ := sshx.GenerateKey()
	bg := context.Background()
	count := "grep -cxF '" + keyB + "' /root/.ssh/authorized_keys || true"
	for i := 0; i < 2; i++ {
		if err := Adopt(bg, root, "root", keyB); err != nil {
			t.Fatal(err)
		}
		if n := testenv.Sh(t, root, count); n != "1" {
			t.Fatalf("round %d: key count %s", i, n)
		}
	}
	if n := testenv.Sh(t, root, "grep -c '^line /root/.ssh/authorized_keys ' /etc/xray/wayveil-manifest"); n != "2" {
		t.Fatalf("manifest line entries (app key + adopted) = %s", n)
	}
	if err := Adopt(bg, root, "nosuchuser", keyB); !errors.Is(err, &ScriptError{Code: "no_such_user"}) {
		t.Fatalf("unknown user: %v", err)
	}
	if err := Uninstall(bg, root, "", nil); err != nil {
		t.Fatal(err)
	}
	if n := testenv.Sh(t, e.Root(t), count); n != "0" {
		t.Fatalf("key left after uninstall: %s", n)
	}
}

func TestAdoptNotInstalled(t *testing.T) {
	e := testenv.Start(t)
	c := e.Connect(t, "root", "wayveil")
	_, line, _ := sshx.GenerateKey()
	if err := Adopt(context.Background(), c, "root", line); !errors.Is(err, &ScriptError{Code: "not_installed"}) {
		t.Fatalf("got %v", err)
	}
}
