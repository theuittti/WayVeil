//go:build integration

package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/theuittti/WayVeil/internal/sshx"
	"github.com/theuittti/WayVeil/internal/testenv"
)

// installed installs on a fresh container as user and returns the root
// client, the app key line and the result.
func installed(t *testing.T, e *testenv.Env, user, pass string) (*sshx.Client, string, InstallResult) {
	t.Helper()
	c := e.Connect(t, user, pass)
	_, line, _ := sshx.GenerateKey()
	var steps []StepEvent
	res, err := Install(context.Background(), c, InstallOptions{
		Address: "203.0.113.10", SNI: "www.microsoft.com", AuthUser: user, AuthKeyLine: line, XrayZip: testenv.XrayZip(),
	}, func(s StepEvent) { steps = append(steps, s) })
	if err != nil {
		var se *ScriptError
		if errors.As(err, &se) {
			t.Logf("message: %s", se.Message)
		}
		t.Logf("journal: %s", testenv.Sh(t, e.Root(t), "journalctl -u xray --no-pager -n 20 2>&1 || true"))
		t.Fatalf("install: %v (steps %v)", err, steps)
	}
	return e.Root(t), line, res
}

func TestInstallRoot(t *testing.T) {
	e := testenv.Start(t)
	root, line, res := installed(t, e, "root", "wayveil")
	if s := testenv.Sh(t, root, "systemctl is-active xray"); s != "active" {
		t.Fatal(s)
	}
	mf := testenv.Sh(t, root, "cat /etc/xray/wayveil-manifest")
	for _, w := range []string{"file /usr/local/bin/xray", "service xray", "user xray", "dir /etc/xray"} {
		if !strings.Contains(mf, w) {
			t.Errorf("manifest lacks %q:\n%s", w, mf)
		}
	}
	if pk := testenv.Sh(t, root, "jq -r .publicKey /etc/xray/wayveil.json"); pk != res.PublicKey || pk == "" {
		t.Fatalf("publicKey %q vs %q", pk, res.PublicKey)
	}
	if n := testenv.Sh(t, root, "grep -cxF '"+line+"' /root/.ssh/authorized_keys"); n != "1" {
		t.Fatalf("key count %s", n)
	}
	if v := testenv.Sh(t, root, "/usr/local/bin/xray version | head -1"); !strings.Contains(v, strings.TrimPrefix(XrayVersion, "v")) {
		t.Fatalf("version %s", v)
	}
}

func TestInstallDeploy(t *testing.T) {
	e := testenv.Start(t)
	root, line, _ := installed(t, e, "deploy", "deploy")
	if n := testenv.Sh(t, root, "grep -cxF '"+line+"' /home/deploy/.ssh/authorized_keys"); n != "1" {
		t.Fatalf("key count %s", n)
	}
	if o := testenv.Sh(t, root, "stat -c %U /home/deploy/.ssh/authorized_keys"); o != "deploy" {
		t.Fatalf("owner %s", o)
	}
}

func TestInstallBBRWarnOK(t *testing.T) {
	e := testenv.Start(t)
	c := e.Root(t)
	_, line, _ := sshx.GenerateKey()
	var bbr StepStatus
	_, err := Install(context.Background(), c, InstallOptions{Address: "x", SNI: "www.microsoft.com", AuthUser: "root", AuthKeyLine: line, XrayZip: testenv.XrayZip()},
		func(s StepEvent) {
			if s.ID == "bbr" && s.Status != StepStart {
				bbr = s.Status
			}
		})
	if err != nil || (bbr != StepOK && bbr != StepWarn) {
		t.Fatalf("err %v bbr %q", err, bbr)
	}
}

func TestInstallTwice(t *testing.T) {
	e := testenv.Start(t)
	root, line, res := installed(t, e, "root", "wayveil")
	_, err := Install(context.Background(), root, InstallOptions{Address: "x", SNI: "www.apple.com", AuthUser: "root", AuthKeyLine: line, XrayZip: testenv.XrayZip()}, nil)
	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("want already_installed, got %v", err)
	}
	if pk := testenv.Sh(t, root, "jq -r .publicKey /etc/xray/wayveil.json"); pk != res.PublicKey {
		t.Fatal("wayveil.json changed")
	}
}

func TestInstallPortBusy(t *testing.T) {
	e := testenv.Start(t)
	c := e.Root(t)
	testenv.Sh(t, c, "nohup python3 -m http.server 443 >/dev/null 2>&1 & sleep 1")
	_, line, _ := sshx.GenerateKey()
	_, err := Install(context.Background(), c, InstallOptions{Address: "x", SNI: "www.microsoft.com", AuthUser: "root", AuthKeyLine: line, XrayZip: testenv.XrayZip()}, nil)
	if !errors.Is(err, ErrPortBusy) {
		t.Fatalf("want port_busy, got %v", err)
	}
	if out := testenv.Sh(t, c, "test -e /usr/local/bin/xray && echo yes || echo no"); out != "no" {
		t.Fatal("xray was installed")
	}
}

// Container has no IPv6: xray must not pick AAAA records at random.
func TestInstallIPv4OnlyStrategy(t *testing.T) {
	root, _, _ := installed(t, testenv.Start(t), "root", "wayveil")
	if s := testenv.Sh(t, root, `jq -r '.outbounds[]|select(.tag=="direct").streamSettings.sockopt.domainStrategy' /etc/xray/config.json`); s != "ForceIPv4" {
		t.Fatalf("domainStrategy %q", s)
	}
	if s := testenv.Sh(t, root, "jq -r .dns.queryStrategy /etc/xray/config.json"); s != "UseIPv4" {
		t.Fatalf("queryStrategy %q", s)
	}
}

// The server downloads xray itself (no uploaded zip): needs GitHub.
func TestInstallDownloadsXray(t *testing.T) {
	c := testenv.Start(t).Root(t)
	_, line, _ := sshx.GenerateKey()
	if _, err := Install(context.Background(), c, InstallOptions{Address: "x", SNI: "www.microsoft.com", AuthUser: "root", AuthKeyLine: line}, nil); err != nil {
		var se *ScriptError
		if errors.As(err, &se) {
			t.Fatalf("%s: %s", se.Code, se.Message)
		}
		t.Fatal(err)
	}
}
