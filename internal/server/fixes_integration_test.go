//go:build integration

package server

import (
	"errors"
	"strings"
	"testing"

	"github.com/wayveil/wayveil/internal/sshx"
	"github.com/wayveil/wayveil/internal/testenv"
)

func has(t *testing.T, c *sshx.Client, path string) bool {
	t.Helper()
	return testenv.Sh(t, c, "test -e "+path+" && echo y || echo n") == "y"
}

// Final review C1: purging Wayveil's deps must not take the user's packages with them.
func TestUninstallKeepsDependents(t *testing.T) {
	root, line, _ := installed(t, testenv.Start(t), "root", "wayveil")
	if !strings.Contains(testenv.Sh(t, root, "cat /etc/xray/wayveil-manifest"), "pkg libonig5") {
		t.Skip("libonig5 not recorded on this image")
	}
	testenv.Sh(t, root, "apt-get install -y -qq --no-install-recommends libonig-dev >/dev/null 2>&1") // user's own package, depends on libonig5
	var steps []StepEvent
	if err := Uninstall(bg, root, line, func(s StepEvent) { steps = append(steps, s) }); err != nil {
		t.Fatal(err)
	}
	if st := testenv.Sh(t, root, "dpkg-query -W -f='${db:Status-Abbrev}' libonig-dev"); !strings.HasPrefix(st, "ii") {
		t.Fatalf("user's libonig-dev was removed (%q); steps %v", st, steps)
	}
	if testenv.Sh(t, root, "dpkg-query -W -f='${db:Status-Abbrev}' unzip 2>/dev/null || true") == "ii " {
		t.Fatal("unrelated recorded package unzip should still be purged")
	}
}

// Final review I1: a failed install must be recoverable: uninstaller and app
// key in place, precheck says partial, uninstall cleans, install works again.
func TestFailedInstallRecoverable(t *testing.T) {
	e := testenv.Start(t)
	root := e.Root(t)
	_, line, _ := sshx.GenerateKey()
	o := InstallOptions{Address: "x", SNI: "www.microsoft.com", AuthUser: "root", AuthKeyLine: line}
	if _, err := installWith(bg, root, o, nil, map[string]string{"WV_FAIL_AT": "service"}); err == nil {
		t.Fatal("expected failure")
	}
	h, err := Precheck(bg, root)
	if err != nil || !h.PartialInstall || h.HasWayveil || h.HasForeignXray {
		t.Fatalf("precheck %+v %v", h, err)
	}
	if _, err := Install(bg, root, o, nil); !errors.Is(err, ErrPartialInstall) {
		t.Fatalf("want partial_install, got %v", err)
	}
	if !has(t, root, "/etc/xray/wayveil-uninstall.sh") {
		t.Fatal("uninstaller missing after failed install")
	}
	if testenv.Sh(t, root, "grep -cxF '"+line+"' /root/.ssh/authorized_keys || true") != "1" {
		t.Fatal("app key must be authorized early so the app can clean up")
	}
	if err := Uninstall(bg, root, line, nil); err != nil {
		t.Fatal(err)
	}
	if has(t, root, "/etc/xray") || has(t, root, "/usr/local/bin/xray") {
		t.Fatal("leftovers after uninstall")
	}
	if _, err := Install(bg, root, o, nil); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
}

// holdLock keeps the dpkg frontend lock (fcntl, like apt) for sec seconds.
func holdLock(t *testing.T, c *sshx.Client, sec string) {
	testenv.Sh(t, c, `nohup python3 -c 'import fcntl,time;f=open("/var/lib/dpkg/lock-frontend","w");fcntl.lockf(f,fcntl.LOCK_EX);time.sleep(`+sec+`)' >/dev/null 2>&1 & sleep 1`)
}

// Final review I2a: cloud-init / unattended-upgrades hold apt on fresh VPSes.
func TestInstallWaitsForAptLock(t *testing.T) {
	root := testenv.Start(t).Root(t)
	holdLock(t, root, "15")
	_, line, _ := sshx.GenerateKey()
	if _, err := Install(bg, root, InstallOptions{Address: "x", SNI: "www.microsoft.com", AuthUser: "root", AuthKeyLine: line}, nil); err != nil {
		t.Fatalf("install while apt busy: %v", err)
	}
}

// Final review I2b: a purge that fails (apt busy) must stay retryable.
func TestUninstallRetryAfterAptBusy(t *testing.T) {
	root, line, _ := installed(t, testenv.Start(t), "root", "wayveil")
	holdLock(t, root, "600")
	var steps []StepEvent
	if err := uninstallWith(bg, root, line, func(s StepEvent) { steps = append(steps, s) }, map[string]string{"WV_APT_LOCK_TIMEOUT": "2"}); err != nil {
		t.Fatal(err)
	}
	mf := testenv.Sh(t, root, "cat /etc/xray/wayveil-manifest 2>/dev/null || true")
	if !strings.Contains(mf, "pkg unzip") || !has(t, root, "/etc/xray/wayveil-uninstall.sh") {
		t.Fatalf("pending packages must stay in the manifest, got:\n%s\nsteps %v\nlock holder: %s", mf, steps, testenv.Sh(t, root, "ps aux | grep -c [f]cntl || true"))
	}
	testenv.Sh(t, root, "pkill -f 'fcntl[.]lockf'; sleep 1") // apt is free again
	if err := Uninstall(bg, root, line, nil); err != nil {
		t.Fatal(err)
	}
	if has(t, root, "/etc/xray") || testenv.Sh(t, root, "dpkg-query -W -f='${db:Status-Abbrev}' unzip 2>/dev/null || true") == "ii " {
		t.Fatal("second uninstall did not finish the job")
	}
}

// Final review I3: authorized_keys without a trailing newline.
func TestAuthKeyNoTrailingNewline(t *testing.T) {
	e := testenv.Start(t)
	c := e.Root(t)
	testenv.Sh(t, c, "mkdir -p /root/.ssh && printf 'ssh-ed25519 AAAAUSERKEY user@laptop' > /root/.ssh/authorized_keys")
	root, line, _ := installed(t, e, "root", "wayveil")
	if testenv.Sh(t, root, "grep -cxF 'ssh-ed25519 AAAAUSERKEY user@laptop' /root/.ssh/authorized_keys") != "1" {
		t.Fatalf("user's key line damaged:\n%s", testenv.Sh(t, root, "cat /root/.ssh/authorized_keys"))
	}
	if err := Uninstall(bg, root, line, nil); err != nil {
		t.Fatal(err)
	}
	if got := testenv.Sh(t, root, "cat /root/.ssh/authorized_keys"); got != "ssh-ed25519 AAAAUSERKEY user@laptop" {
		t.Fatalf("after uninstall: %q", got)
	}
}

// Final review I4: /etc/xray that Wayveil did not create must survive.
func TestUninstallKeepsForeignXrayDir(t *testing.T) {
	root := testenv.Start(t).Root(t)
	testenv.Sh(t, root, "mkdir -p /etc/xray && echo keep > /etc/xray/other.json")
	if _, err := root.Run(bg, "bash -s", script("uninstall")); err != nil {
		t.Fatal(err)
	}
	if !has(t, root, "/etc/xray/other.json") {
		t.Fatal("uninstall without manifest deleted /etc/xray")
	}
	_, line, _ := sshx.GenerateKey()
	if _, err := Install(bg, root, InstallOptions{Address: "x", SNI: "www.microsoft.com", AuthUser: "root", AuthKeyLine: line}, nil); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(bg, root, line, nil); err != nil {
		t.Fatal(err)
	}
	if !has(t, root, "/etc/xray/other.json") || has(t, root, "/etc/xray/config.json") {
		t.Fatal("pre-existing /etc/xray must keep foreign files and lose ours")
	}
}
