//go:build e2e

package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/theuittti/WayVeil/internal/remote"
	"github.com/theuittti/WayVeil/internal/server"
	"github.com/theuittti/WayVeil/internal/sshx"
	"github.com/theuittti/WayVeil/internal/testenv"
	"github.com/theuittti/WayVeil/rules"
)

// End-to-end on the user's disposable test VPS (~/.config/wayveil-test/vps.env).
// The password is used once to authorize a persistent test key and is never
// printed. Run with: make build e2e
func TestE2EVPS(t *testing.T) {
	dir := filepath.Join(os.Getenv("HOME"), ".config", "wayveil-test")
	env, err := readEnv(filepath.Join(dir, "vps.env"))
	if err != nil {
		t.Skip("no ~/.config/wayveil-test/vps.env: ", err)
	}
	tm := newTimer(t)
	port, _ := strconv.Atoi(env["WV_TEST_PORT"])
	host, user := env["WV_TEST_HOST"], env["WV_TEST_USER"]

	done := tm.phase("ssh connect")
	root := vpsConnect(t, dir, host, port, user, env["WV_TEST_PASS"])
	done()
	if out, err := root.Run(bg, vpsSpecCmd, nil); err == nil {
		t.Logf("VPS: %s", strings.TrimSpace(string(out)))
	}

	vpsIP := host
	if net.ParseIP(host) == nil {
		ips, err := net.LookupHost(host)
		if err != nil {
			t.Fatal(err)
		}
		vpsIP = ips[0]
	}

	// Start from a server that has never seen Wayveil: our uninstaller first,
	// then a wipe of anything earlier (broken) attempts may have left behind.
	// Unless WV_TEST_KEEP_DEPS=1, the packages the installer would add and the
	// apt package lists are removed too, so the install below pays the full
	// price a fresh cheap VPS pays.
	done = tm.phase("uninstall (previous run)")
	if err := server.Uninstall(bg, root, "", logSteps(t, "uninstall")); err != nil {
		t.Logf("uninstall: %v (the wipe below cleans up)", err)
	}
	done()
	done = tm.phase("wipe leftovers")
	wipe := wipeScript
	if env["WV_TEST_KEEP_DEPS"] != "1" {
		wipe += wipeDepsScript
	}
	out, err := root.Run(bg, "bash -s", []byte(wipe))
	if err != nil {
		t.Fatalf("wipe: %v\n%s", err, out)
	}
	t.Logf("wipe:\n%s", strings.TrimSpace(string(out)))
	done()

	done = tm.phase("precheck")
	h, err := server.Precheck(bg, root)
	done()
	if err != nil {
		t.Fatal(err)
	}
	if !h.Supported || h.Port443Busy || h.HasWayveil || h.PartialInstall || h.HasForeignXray {
		t.Fatalf("test VPS not clean/usable: %+v", h)
	}

	done = tm.phase("target scan")
	sni := "www.microsoft.com"
	var picked bool
	server.SuggestTargets(bg, root, h, rules.DefaultTargets(), func(s server.Suggestion) {
		if !picked && s.Recommended {
			sni, picked = s.Domain, true
		}
	})
	done()
	t.Logf("target: %s (recommended by scan: %v)", sni, picked)

	zip := testenv.XrayZip()
	if len(zip) == 0 {
		t.Fatal("build/Xray-linux-64.zip missing: run make build (the VPS must not depend on GitHub)")
	}
	_, appKey, _ := sshx.GenerateKey() // throwaway "app" key; our test key stays outside the manifest
	var steps []string
	done = tm.phase("install")
	_, err = server.Install(bg, root, server.InstallOptions{Address: vpsIP, SNI: sni, AuthUser: user, AuthKeyLine: appKey, XrayZip: zip},
		func(st server.StepEvent) {
			steps = append(steps, st.ID+":"+string(st.Status)+" "+st.Message)
			t.Logf("  install step %-12s %s %s (%s)", st.ID, st.Status, st.Message, time.Now().Format("15:04:05"))
		})
	done()
	if err != nil {
		var se *server.ScriptError
		if errors.As(err, &se) {
			t.Logf("install failed: %s: %s", se.Code, se.Message)
		}
		out, _ := root.Run(bg, "ls -t /tmp/wayveil-install.* 2>/dev/null | head -1 | xargs -r tail -n 30; "+
			"echo '--- apt/dpkg holders:'; ps -eo pid,etime,cmd | grep -E '[a]pt|[d]pkg|[u]nattended' | head", nil)
		t.Fatalf("install: %v\nsteps: %v\nserver log:\n%s", err, steps, out)
	}
	done = tm.phase("add client")
	cl, err := server.AddClient(bg, root, "e2e")
	if err != nil {
		t.Fatal(err)
	}
	info, err := server.Info(bg, root)
	if err != nil {
		t.Fatal(err)
	}
	done()

	done = tm.phase("local client container (docker build+run)")
	c := testenv.StartClient(t)
	done()
	myIP := c.App(t, "curl -s --max-time 15 https://api.ipify.org")
	done = tm.phase("client import + rules")
	c.App(t, "./wayveil import '"+server.ClientLink(info, cl)+"'")
	c.App(t, "./wayveil rules proxy ipify.org && ./wayveil rules direct ifconfig.me")
	done()
	done = tm.phase("client connect")
	c.App(t, "./wayveil connect")
	done()
	t.Cleanup(func() { c.AppTry(t, "./wayveil disconnect") })

	done = tm.phase("routing checks")
	defer done()
	if got := c.App(t, "curl -s --max-time 20 https://api.ipify.org"); got != vpsIP {
		t.Errorf("proxied site sees %q, want VPS IP", got)
	}
	if got := c.App(t, "curl -s --max-time 20 https://ifconfig.me/ip"); got != myIP {
		t.Errorf("direct site sees %q, want own IP %q", got, myIP)
	}
	if code := c.AppTry(t, "curl -s -o /dev/null -w '%{http_code}' --max-time 20 https://www.youtube.com/"); !strings.HasPrefix(code, "2") && !strings.HasPrefix(code, "3") {
		t.Errorf("youtube via VPS: HTTP %q", code)
	}
	for _, u := range []struct{ url, host string }{
		{"https://chatgpt.com/", "chatgpt.com"},
		{"https://www.instagram.com/", "www.instagram.com"},
	} {
		if got := route(t, c, u.url, u.host, ""); got != "proxy" {
			t.Errorf("%s via %s", u.host, got)
		}
	}
	if got := route(t, c, "https://mirror.yandex.ru/", "mirror.yandex.ru", ""); got != "direct" {
		t.Errorf("mirror.yandex.ru via %s", got)
	}
	// The server installation is left in place; the next run wipes it first.
}

const vpsSpecCmd = `. /etc/os-release; echo "$PRETTY_NAME, $(uname -m), $(nproc) vCPU, $(free -m | awk '/^Mem:/{print $2}') MB RAM"`

// wipeScript removes every trace of earlier Wayveil attempts that the
// manifest-driven uninstaller could not know about. Disposable VPS only.
const wipeScript = `set -u
found=""
for p in /etc/xray /usr/local/bin/xray /etc/systemd/system/xray.service /var/log/xray \
         /etc/logrotate.d/xray /etc/sysctl.d/99-xray-bbr.conf /root/.cache/wayveil; do
  [ -e "$p" ] && found="$found $p"
done
ls /tmp/wayveil-* >/dev/null 2>&1 && found="$found /tmp/wayveil-*"
getent passwd xray >/dev/null && found="$found user:xray"
systemctl disable --now xray >/dev/null 2>&1
rm -rf /etc/xray /usr/local/bin/xray /etc/systemd/system/xray.service /var/log/xray \
       /etc/logrotate.d/xray /etc/sysctl.d/99-xray-bbr.conf /root/.cache/wayveil /tmp/wayveil-*
systemctl daemon-reload; systemctl reset-failed xray >/dev/null 2>&1
getent passwd xray >/dev/null && userdel xray
# app keys of earlier runs end in " wayveil"; the test's own key ends in " wayveil-e2e"
keys=0
for ak in /root/.ssh/authorized_keys /home/*/.ssh/authorized_keys; do
  [ -f "$ak" ] || continue
  n=$(grep -c ' wayveil$' "$ak"); keys=$((keys + n))
  [ "$n" -gt 0 ] && sed -i '/ wayveil$/d' "$ak"
done
[ "$keys" -gt 0 ] && found="$found app-keys:$keys"
echo "leftovers removed:${found:- none}"
true
`

// wipeDepsScript removes what the installer's deps step would install
// (curl and logrotate are part of every base image and stay) and the apt
// lists, so the next install runs a full apt-get update.
const wipeDepsScript = `export DEBIAN_FRONTEND=noninteractive
pre=""
for p in jq libjq1 libonig5 unzip; do dpkg -s "$p" >/dev/null 2>&1 && pre="$pre $p"; done
[ -n "$pre" ] && apt-get purge -y -qq $pre >/dev/null 2>&1
rm -rf /var/lib/apt/lists/*
echo "packages purged:${pre:- none}; apt lists cleared"
true
`

// timer records how long each phase of the e2e run takes and prints a
// summary when the test ends (also on failure).
type timer struct {
	t      *testing.T
	start  time.Time
	phases []string
	durs   []time.Duration
}

func newTimer(t *testing.T) *timer {
	tm := &timer{t: t, start: time.Now()}
	t.Cleanup(func() {
		var b strings.Builder
		for i, p := range tm.phases {
			fmt.Fprintf(&b, "  %-44s %7.1fs\n", p, tm.durs[i].Seconds())
		}
		fmt.Fprintf(&b, "  %-44s %7.1fs", "total", time.Since(tm.start).Seconds())
		t.Logf("timings:\n%s", b.String())
	})
	return tm
}

// phase starts a phase; call the returned func when it ends.
func (tm *timer) phase(name string) func() {
	s := time.Now()
	tm.t.Logf("▶ %s (%s)", name, s.Format("15:04:05"))
	return func() {
		d := time.Since(s).Round(100 * time.Millisecond)
		tm.phases, tm.durs = append(tm.phases, name), append(tm.durs, d)
		tm.t.Logf("■ %s: %s", name, d)
	}
}

func logSteps(t *testing.T, what string) func(server.StepEvent) {
	return func(st server.StepEvent) {
		if st.Status != server.StepStart {
			t.Logf("  %s step %-12s %s %s", what, st.ID, st.Status, st.Message)
		}
	}
}

func readEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "="); ok && !strings.HasPrefix(k, "#") {
			m[k] = strings.Trim(v, `"'`)
		}
	}
	if m["WV_TEST_HOST"] == "" {
		return nil, errors.New("WV_TEST_HOST missing")
	}
	if m["WV_TEST_PORT"] == "" {
		m["WV_TEST_PORT"] = "22"
	}
	if m["WV_TEST_USER"] == "" {
		m["WV_TEST_USER"] = "root"
	}
	return m, nil
}

// vpsConnect uses the persistent test key; on the first run it logs in with
// the password, authorizes that key and pins the host key.
func vpsConnect(t *testing.T, dir, host string, port int, user, pass string) *sshx.Client {
	t.Helper()
	keyPath, hostKeyPath := filepath.Join(dir, "id_ed25519"), filepath.Join(dir, "host_key")
	key, kerr := os.ReadFile(keyPath)
	hk, _ := os.ReadFile(hostKeyPath)
	try := func(tg sshx.Target) (*sshx.Client, error) {
		var last error
		for i := 0; i < 3; i++ {
			c, err := sshx.Connect(context.Background(), tg)
			if err == nil {
				return c, nil
			}
			last = err
			time.Sleep(5 * time.Second)
		}
		return nil, last
	}
	if kerr == nil {
		c, err := try(sshx.Target{Host: host, Port: port, User: user, PrivateKeyPEM: key, HostKey: strings.TrimSpace(string(hk))})
		if err != nil {
			t.Fatalf("connect with test key: %v", err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	if pass == "" {
		t.Skip("no test key yet and WV_TEST_PASS is empty")
	}
	c, err := try(sshx.Target{Host: host, Port: port, User: user, Password: pass})
	if err != nil {
		t.Fatalf("first connect with password: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	pemKey, line, err := sshx.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	line = strings.TrimSuffix(line, " wayveil") + " wayveil-e2e"
	if _, err := c.Run(bg, "mkdir -p ~/.ssh && chmod 700 ~/.ssh && "+
		"{ [ -s ~/.ssh/authorized_keys ] && [ -n \"$(tail -c1 ~/.ssh/authorized_keys)\" ] && echo >> ~/.ssh/authorized_keys; true; } && "+
		"echo "+remote.ShellQuote(line)+" >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys", nil); err != nil {
		t.Fatalf("authorize test key: %v", err)
	}
	if err := os.WriteFile(keyPath, pemKey, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostKeyPath, []byte(c.HostKey()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("test key authorized (fingerprint of host: %s); WV_TEST_PASS can now be removed from vps.env", c.Fingerprint())
	return c
}
