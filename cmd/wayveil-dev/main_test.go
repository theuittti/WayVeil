package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theuittti/WayVeil/internal/ownerstore"
	"github.com/theuittti/WayVeil/internal/remote"
	"github.com/theuittti/WayVeil/internal/server"
	"github.com/theuittti/WayVeil/internal/sshx"
)

type fakeConn struct{ remote.Runner }

func (fakeConn) HostKey() string     { return "ssh-ed25519 AAAAHOST" }
func (fakeConn) Fingerprint() string { return "SHA256:abc" }
func (fakeConn) Close() error        { return nil }

func TestCLINoPasswordInProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const pw = "s3cr3t-pass"
	var out bytes.Buffer
	var gotTarget sshx.Target
	var gotOpts server.InstallOptions
	d := deps{
		out:          &out,
		readPassword: func() (string, error) { return pw, nil },
		confirm:      func(string) bool { return true },
		connect: func(_ context.Context, tg sshx.Target) (conn, error) {
			gotTarget = tg
			return fakeConn{}, nil
		},
		install: func(_ context.Context, _ remote.Runner, o server.InstallOptions, _ func(server.StepEvent)) (server.InstallResult, error) {
			gotOpts = o
			return server.InstallResult{PublicKey: "PBK", ShortID: "ab12"}, nil
		},
		addClient: func(_ context.Context, _ remote.Runner, name string) (server.Client, error) {
			return server.Client{ID: "c-1", UUID: "uuid-1", Name: name}, nil
		},
		info: func(context.Context, remote.Runner) (server.ServerInfo, error) {
			return server.ServerInfo{Address: "203.0.113.10", SNI: "www.nu.nl", PublicKey: "PBK", ShortID: "ab12"}, nil
		},
	}
	if code := run([]string{"install", "-host", "203.0.113.10", "-sni", "www.nu.nl"}, d); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if gotTarget.Password != pw || gotOpts.SNI != "www.nu.nl" || gotOpts.Address != "203.0.113.10" ||
		!strings.HasSuffix(gotOpts.AuthKeyLine, " wayveil") || gotOpts.AuthUser != "root" {
		t.Fatalf("target %+v opts %+v", gotTarget, gotOpts)
	}
	if !strings.Contains(out.String(), "vless://uuid-1@203.0.113.10:443") || !strings.Contains(out.String(), "SHA256:abc") {
		t.Fatalf("output:\n%s", out.String())
	}
	p, err := ownerstore.Load("203.0.113.10", 22)
	if err != nil || p.HostKey != "ssh-ed25519 AAAAHOST" || len(p.PrivateKeyPEM) == 0 || p.AuthorizedLine != gotOpts.AuthKeyLine {
		t.Fatalf("profile %+v %v", p, err)
	}
	dir, _ := ownerstore.Dir()
	files, _ := filepath.Glob(filepath.Join(dir, "servers", "*"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if bytes.Contains(b, []byte(pw)) {
			t.Fatalf("password stored in %s", f)
		}
	}
}

func TestCLIRefusesUnconfirmedHostKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var out bytes.Buffer
	installed := false
	d := deps{
		out: &out, readPassword: func() (string, error) { return "x", nil },
		confirm: func(string) bool { return false },
		connect: func(context.Context, sshx.Target) (conn, error) { return fakeConn{}, nil },
		install: func(context.Context, remote.Runner, server.InstallOptions, func(server.StepEvent)) (server.InstallResult, error) {
			installed = true
			return server.InstallResult{}, nil
		},
	}
	if code := run([]string{"install", "-host", "h", "-sni", "s"}, d); code == 0 || installed {
		t.Fatalf("code %d installed %v", code, installed)
	}
}
