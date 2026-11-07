//go:build integration

package server

import (
	"testing"

	"github.com/theuittti/WayVeil/internal/testenv"
)

const pkgList = `dpkg-query -W -f='${db:Status-Abbrev} ${Package}\n' | awk '$1=="ii"{print $2}' | sort`

var serverPaths = []string{
	"/usr/local/bin/xray", "/etc/xray", "/etc/systemd/system/xray.service",
	"/etc/sysctl.d/99-xray-bbr.conf", "/etc/logrotate.d/xray", "/var/log/xray",
}

func TestUninstallClean(t *testing.T) {
	e := testenv.Start(t)
	root0 := e.Root(t)
	pkgsBefore := testenv.Sh(t, root0, pkgList)
	usersBefore := testenv.Sh(t, root0, "getent passwd | cut -d: -f1 | sort")

	root, line, _ := installed(t, e, "root", "wayveil")
	if _, err := AddClient(bg, root, "Вася"); err != nil {
		t.Fatal(err)
	}
	if testenv.Sh(t, root, "test -x /etc/xray/wayveil-uninstall.sh && echo yes") != "yes" {
		t.Fatal("uninstaller copy missing")
	}
	var steps []StepEvent
	if err := Uninstall(bg, root, line, func(s StepEvent) { steps = append(steps, s) }); err != nil {
		t.Fatalf("%v %v", err, steps)
	}
	for _, p := range serverPaths {
		if testenv.Sh(t, root, "test -e "+p+" && echo left || echo gone") != "gone" {
			t.Errorf("%s still exists", p)
		}
	}
	if testenv.Sh(t, root, "systemctl cat xray >/dev/null 2>&1 && echo yes || echo no") != "no" {
		t.Error("xray unit still known to systemd")
	}
	if got := testenv.Sh(t, root, "getent passwd | cut -d: -f1 | sort"); got != usersBefore {
		t.Errorf("users differ: %v", steps)
	}
	if got := testenv.Sh(t, root, pkgList); got != pkgsBefore {
		t.Errorf("packages differ:\n%s", testenv.Sh(t, root, "diff <(echo "+shq(pkgsBefore)+") <("+pkgList+") || true"))
	}
	if testenv.Sh(t, root, "grep -cxF '"+line+"' /root/.ssh/authorized_keys || true") != "0" {
		t.Error("app key still authorized")
	}
	if testenv.Sh(t, root, "ss -ltnH 'sport = :443'") != "" {
		t.Error("port 443 still listening")
	}
	installed(t, e, "root", "wayveil") // reinstall from scratch works
}

func TestUninstallTwice(t *testing.T) {
	root, line, _ := installed(t, testenv.Start(t), "root", "wayveil")
	if err := Uninstall(bg, root, line, nil); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(bg, root, line, nil); err != nil {
		t.Fatalf("second uninstall: %v", err)
	}
}

func TestUninstallManualCopy(t *testing.T) {
	root, _, _ := installed(t, testenv.Start(t), "root", "wayveil")
	testenv.Sh(t, root, "bash /etc/xray/wayveil-uninstall.sh >/dev/null")
	if testenv.Sh(t, root, "test -e /usr/local/bin/xray && echo left || echo gone") != "gone" {
		t.Fatal("xray binary left")
	}
}

func shq(s string) string { return "'" + s + "'" }
