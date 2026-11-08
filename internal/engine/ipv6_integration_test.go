//go:build integration

package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/theuittti/WayVeil/internal/server"
	"github.com/theuittti/WayVeil/internal/sshx"
	"github.com/theuittti/WayVeil/internal/testenv"
)

// IPv6 must never bypass the tunnel: the TUN has no IPv6 address, so
// strict-route has to keep public IPv6 traffic (DNS above all) from leaving
// via the physical interface. 2001:db8::/64 counts as "public" for our rules.
func TestTUNIPv6NoBypass(t *testing.T) {
	e := testenv.Start(t)
	root := e.Root(t)
	_, line, _ := sshx.GenerateKey()
	if _, err := server.Install(bg, root, server.InstallOptions{Address: e.IP(t), SNI: "www.microsoft.com", AuthUser: "root", AuthKeyLine: line}, nil); err != nil {
		t.Fatal(err)
	}
	cl, _ := server.AddClient(bg, root, "v6")
	info, _ := server.Info(bg, root)

	net6 := testenv.Network(t, "2001:db8:7779::/64")
	srv6 := testenv.Attach(t, net6, e.Container)
	e.Refresh(t) // attaching a network can re-create the port mapping
	root = e.Root(t)
	// UDP/53 recorder and an HTTP server on the server's IPv6 address
	testenv.Sh(t, root, `cat > /tmp/udp53.py <<'EOF2'
import socket
s = socket.socket(socket.AF_INET6, socket.SOCK_DGRAM)
s.bind(("::", 53))
while True:
    d, a = s.recvfrom(2048)
    open("/tmp/udp53.log", "a").write(a[0] + "\n")
EOF2
(nohup python3 /tmp/udp53.py >/dev/null 2>&1 &); (nohup python3 -m http.server 8080 --bind :: >/dev/null 2>&1 &); sleep 1; touch /tmp/udp53.log`)

	c := testenv.StartClient(t)
	cli6 := testenv.Attach(t, net6, c.ID)
	t.Logf("server %s, client %s", srv6, cli6)
	if got := c.AppTry(t, "curl -s -o /dev/null -w '%{http_code}' --max-time 5 http://["+srv6+"]:8080/"); got != "200" {
		t.Fatalf("sanity: IPv6 path must work before connecting, got %q", got)
	}
	c.App(t, "./wayveil import '"+server.ClientLink(info, cl)+"' && ./wayveil connect")

	// DNS over IPv6 to a "public" resolver must not leave the machine
	c.AppTry(t, "for i in 1 2 3; do printf 'x' > /dev/udp/"+srv6+"/53; done; sleep 2")
	if got := testenv.Sh(t, root, "cat /tmp/udp53.log"); strings.Contains(got, cli6) {
		t.Errorf("plain DNS over IPv6 reached the network directly (leak): %q", got)
	}
	// TCP over IPv6: either blocked or handled by mihomo — never a silent bypass
	code := c.AppTry(t, "curl -s -o /dev/null -w '%{http_code}' --max-time 8 http://["+srv6+"]:8080/")
	seen := strings.Contains(c.App(t, "cat ~/.cache/wayveil/run/mihomo.log"), srv6)
	t.Logf("IPv6 TCP while connected: http=%s, seen by mihomo=%v", code, seen)
	if code == "200" && !seen {
		t.Errorf("IPv6 TCP bypassed the tunnel")
	}

	// kill -9 + disconnect must not leave IPv6 broken
	c.Root(t, "pkill -9 -x mihomo; sleep 1")
	c.App(t, "./wayveil disconnect")
	time.Sleep(time.Second)
	if got := c.AppTry(t, "curl -s -o /dev/null -w '%{http_code}' --max-time 5 http://["+srv6+"]:8080/"); got != "200" {
		t.Errorf("IPv6 broken after kill -9 + disconnect (http %q)\nip -6 rule:\n%s", got, c.Root(t, "ip -6 rule"))
	}
}
