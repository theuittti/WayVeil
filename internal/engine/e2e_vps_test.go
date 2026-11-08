//go:build e2e

package engine

import (
	"bufio"
	"context"
	"errors"
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
	port, _ := strconv.Atoi(env["WV_TEST_PORT"])
	host, user := env["WV_TEST_HOST"], env["WV_TEST_USER"]
	root := vpsConnect(t, dir, host, port, user, env["WV_TEST_PASS"])

	vpsIP := host
	if net.ParseIP(host) == nil {
		ips, err := net.LookupHost(host)
		if err != nil {
			t.Fatal(err)
		}
		vpsIP = ips[0]
	}

	// Fresh install with the best suggested target.
	_ = server.Uninstall(bg, root, "", nil)
	h, err := server.Precheck(bg, root)
	if err != nil {
		t.Fatal(err)
	}
	if !h.Supported || h.Port443Busy {
		t.Fatalf("test VPS not usable: %+v", h)
	}
	sni := "www.microsoft.com"
	var picked bool
	server.SuggestTargets(bg, root, h, rules.DefaultTargets(), func(s server.Suggestion) {
		if !picked && s.Recommended {
			sni, picked = s.Domain, true
		}
	})
	t.Logf("target: %s", sni)
	_, appKey, _ := sshx.GenerateKey() // throwaway "app" key; our test key stays outside the manifest
	if _, err := server.Install(bg, root, server.InstallOptions{Address: vpsIP, SNI: sni, AuthUser: user, AuthKeyLine: appKey}, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	cl, err := server.AddClient(bg, root, "e2e")
	if err != nil {
		t.Fatal(err)
	}
	info, err := server.Info(bg, root)
	if err != nil {
		t.Fatal(err)
	}

	c := testenv.StartClient(t)
	myIP := c.App(t, "curl -s --max-time 15 https://api.ipify.org")
	c.App(t, "./wayveil import '"+server.ClientLink(info, cl)+"'")
	c.App(t, "./wayveil rules proxy ipify.org && ./wayveil rules direct ifconfig.me")
	c.App(t, "./wayveil connect")
	t.Cleanup(func() { c.AppTry(t, "./wayveil disconnect") })

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
	// The server installation is left in place; the next run reinstalls it.
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
