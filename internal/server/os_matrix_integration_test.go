//go:build integration

package server

import (
	"context"
	"strings"
	"testing"

	"github.com/theuittti/WayVeil/internal/sshx"
	"github.com/theuittti/WayVeil/internal/testenv"
)

// Debian 11 reached end of life (LTS ended 2026-08): its apt mirrors 404, so it
// is refused by precheck instead of failing halfway through the install.
func TestPrecheckRefusesDebian11(t *testing.T) {
	root := testenv.StartOS(t, "debian:12").Root(t)
	testenv.Sh(t, root, `sed -i 's/^VERSION_ID=.*/VERSION_ID="11"/' /etc/os-release`)
	if h, err := Precheck(bg, root); err != nil || h.Supported {
		t.Fatalf("debian 11 must be unsupported: %+v %v", h, err)
	}
}

// Full server lifecycle on every supported OS: precheck → install → client →
// real traffic through the tunnel → uninstall leaves nothing behind.
func TestOSMatrix(t *testing.T) {
	for _, base := range []string{"ubuntu:22.04", "ubuntu:26.04", "debian:12", "debian:13"} {
		t.Run(strings.ReplaceAll(base, ":", "-"), func(t *testing.T) {
			e := testenv.StartOS(t, base)
			root := e.Root(t)
			pkgsBefore := testenv.Sh(t, root, pkgList)
			h, err := Precheck(bg, root)
			if err != nil || !h.Supported {
				t.Fatalf("precheck %+v %v", h, err)
			}
			_, line, _ := sshx.GenerateKey()
			if _, err := Install(context.Background(), root, InstallOptions{Address: "127.0.0.1", SNI: "www.microsoft.com", AuthUser: "root", AuthKeyLine: line, XrayZip: testenv.XrayZip()}, nil); err != nil {
				var se *ScriptError
				if asScript(err, &se) {
					t.Fatalf("install: %s: %s", se.Code, se.Message)
				}
				t.Fatalf("install: %v", err)
			}
			c, err := AddClient(bg, root, "Вася")
			if err != nil {
				t.Fatal(err)
			}
			info, _ := Info(bg, root)
			if _, err := root.Run(bg, "cat > /tmp/client.json", []byte(clientConfig(info, c))); err != nil {
				t.Fatal(err)
			}
			testenv.Sh(t, root, "nohup /usr/local/bin/xray run -c /tmp/client.json >/tmp/client.log 2>&1 & sleep 2")
			if code := testenv.Sh(t, root, "curl -s -o /dev/null -w '%{http_code}' --max-time 20 -x socks5h://127.0.0.1:1080 https://www.microsoft.com/ || true"); code == "000" || code == "" {
				t.Fatalf("traffic through the tunnel failed: %q", code)
			}
			testenv.Sh(t, root, "pkill -f '^/usr/local/bin/xray run -c /tmp/client.json' || true")
			if tr, err := TrafficAll(bg, root); err != nil || tr[c.ID].Down == 0 {
				t.Fatalf("traffic accounting %+v %v", tr, err)
			}
			if err := Uninstall(bg, root, line, nil); err != nil {
				t.Fatal(err)
			}
			for _, p := range serverPaths {
				if testenv.Sh(t, root, "test -e "+p+" && echo left || echo gone") != "gone" {
					t.Errorf("%s left after uninstall", p)
				}
			}
			if got := testenv.Sh(t, root, pkgList); got != pkgsBefore {
				t.Errorf("package set changed after uninstall")
			}
		})
	}
}

func asScript(err error, se **ScriptError) bool {
	s, ok := err.(*ScriptError)
	if ok {
		*se = s
	}
	return ok
}
