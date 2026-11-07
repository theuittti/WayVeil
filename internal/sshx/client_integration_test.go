//go:build integration

package sshx_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/theuittti/WayVeil/internal/remote"
	"github.com/theuittti/WayVeil/internal/sshx"
	"github.com/theuittti/WayVeil/internal/testenv"
)

func TestRootPasswordRun(t *testing.T) {
	e := testenv.Start(t)
	c := e.Root(t)
	if got := testenv.Sh(t, c, "id -u"); got != "0" {
		t.Fatalf("id -u = %q", got)
	}
	if !strings.HasPrefix(c.Fingerprint(), "SHA256:") {
		t.Fatal(c.Fingerprint())
	}
}

func TestDeploySudo(t *testing.T) {
	e := testenv.Start(t)
	if got := testenv.Sh(t, e.Connect(t, "deploy", "deploy"), "id -u"); got != "0" {
		t.Fatalf("id -u = %q", got)
	}
}

func TestNopwSudo(t *testing.T) {
	e := testenv.Start(t)
	_, err := e.Connect(t, "nopw", "nopw").Run(context.Background(), "id -u", nil)
	if !errors.Is(err, remote.ErrSudoPassword) {
		t.Fatalf("want ErrSudoPassword, got %v", err)
	}
}

func TestHostKeyMismatch(t *testing.T) {
	e := testenv.Start(t)
	_, other, _ := sshx.GenerateKey()
	tg := e.Target("root", "wayveil")
	tg.HostKey = strings.TrimSuffix(other, " wayveil")
	if _, err := sshx.Connect(context.Background(), tg); !errors.Is(err, sshx.ErrHostKeyMismatch) {
		t.Fatalf("want mismatch, got %v", err)
	}
}

func TestPinnedHostKeyAndKeyAuth(t *testing.T) {
	e := testenv.Start(t)
	c := e.Root(t)
	pemKey, line, _ := sshx.GenerateKey()
	testenv.Sh(t, c, "mkdir -p ~/.ssh && echo "+remote.ShellQuote(line)+" >> ~/.ssh/authorized_keys")
	tg := sshx.Target{Host: e.Host, Port: e.Port, User: "root", PrivateKeyPEM: pemKey, HostKey: c.HostKey()}
	c2, err := sshx.Connect(context.Background(), tg)
	if err != nil {
		t.Fatal(err)
	}
	c2.Close()
}

func TestStreamLines(t *testing.T) {
	e := testenv.Start(t)
	var lines []string
	err := e.Root(t).Stream(context.Background(), "printf 'a\\nb\\n'; echo err >&2; exit 3", nil,
		func(s string) { lines = append(lines, s) })
	var ee *remote.ExitError
	if !errors.As(err, &ee) || ee.Code != 3 || ee.Stderr != "err\n" {
		t.Fatalf("err %v", err)
	}
	if !slices.Equal(lines, []string{"a", "b"}) {
		t.Fatalf("lines %q", lines)
	}
}

func TestStdin(t *testing.T) {
	e := testenv.Start(t)
	out, err := e.Root(t).Run(context.Background(), "cat", []byte("x'y"))
	if err != nil || string(out) != "x'y" {
		t.Fatalf("%q %v", out, err)
	}
}
