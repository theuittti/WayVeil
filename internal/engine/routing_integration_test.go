//go:build integration

package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/theuittti/WayVeil/internal/server"
	"github.com/theuittti/WayVeil/internal/sshx"
	"github.com/theuittti/WayVeil/internal/testenv"
)

// tunClient installs a Wayveil server in one container and connects a TUN
// client container to it with `wayveil import` + `wayveil connect`.
func tunClient(t *testing.T) *testenv.Client {
	c, _ := tunClientWithServer(t)
	return c
}

// tunClientWithServer also returns a root shell on the server, so tests can
// check the server's access log: proof that traffic really went through it.
func tunClientWithServer(t *testing.T) (*testenv.Client, *sshx.Client) {
	t.Helper()
	e := testenv.Start(t)
	root := e.Root(t)
	_, line, _ := sshx.GenerateKey()
	if _, err := server.Install(bg, root, server.InstallOptions{Address: e.IP(t), SNI: "www.microsoft.com", AuthUser: "root", AuthKeyLine: line}, nil); err != nil {
		t.Fatal(err)
	}
	cl, err := server.AddClient(bg, root, "it")
	if err != nil {
		t.Fatal(err)
	}
	info, err := server.Info(bg, root)
	if err != nil {
		t.Fatal(err)
	}
	c := testenv.StartClient(t)
	c.App(t, "./wayveil import '"+server.ClientLink(info, cl)+"'")
	c.App(t, "./wayveil connect")
	t.Cleanup(func() { c.AppTry(t, "./wayveil disconnect") })
	return c, root
}

type conn struct {
	Metadata struct {
		Host          string `json:"host"`
		DestinationIP string `json:"destinationIP"`
	} `json:"metadata"`
	Chains []string `json:"chains"`
}

// route starts a slow request (so the connection stays open) and returns the
// outbound mihomo picked for it: "proxy" or "direct".
func route(t *testing.T, c *testenv.Client, url, host, ip string) string {
	t.Helper()
	c.App(t, "setsid nohup curl -sk --limit-rate 200 --max-time 30 -o /dev/null -w '%{http_code} %{time_connect}s' "+url+" > /tmp/bg.txt 2>&1 < /dev/null &")
	var run struct{ ClashAddr, ClashSecret string }
	json.Unmarshal([]byte(c.App(t, "cat ~/.cache/wayveil/run/run.json")), &run)
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		raw := c.AppTry(t, "curl -s -H 'Authorization: Bearer "+run.ClashSecret+"' http://"+run.ClashAddr+"/connections")
		var r struct{ Connections []conn }
		if json.Unmarshal([]byte(raw), &r) == nil {
			for _, x := range r.Connections {
				if (host != "" && x.Metadata.Host == host) || (ip != "" && x.Metadata.DestinationIP == ip) {
					if strings.EqualFold(x.Chains[0], "DIRECT") {
						return "direct"
					}
					return "proxy" // chains are ["vless", "proxy"]
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Logf("background curl: %s", c.AppTry(t, "cat /tmp/bg.txt"))
	var last struct{ Connections []conn }
	json.Unmarshal([]byte(c.AppTry(t, "curl -s -H 'Authorization: Bearer "+run.ClashSecret+"' http://"+run.ClashAddr+"/connections")), &last)
	for _, x := range last.Connections {
		t.Logf("conn host=%q ip=%q chains=%v", x.Metadata.Host, x.Metadata.DestinationIP, x.Chains)
	}
	t.Logf("curl direct: %s", c.AppTry(t, "curl -sk -o /dev/null -w '%{http_code} %{remote_ip}' --max-time 15 "+url))
	t.Logf("mihomo.log: %s", c.AppTry(t, "grep -vE 'provider|Geo|initial' ~/.cache/wayveil/run/mihomo.log | tail -n 15"))
	t.Logf("ps: %s", c.AppTry(t, "ps aux | grep -E '[c]url|[m]ihomo'"))
	t.Fatalf("no connection seen for %s", url)
	return ""
}

func TestTUNSmartRouting(t *testing.T) {
	c, root := tunClientWithServer(t)
	if got := route(t, c, "https://www.youtube.com/", "www.youtube.com", ""); got != "proxy" {
		t.Errorf("youtube via %s", got)
	}
	if got := route(t, c, "https://mirror.yandex.ru/", "mirror.yandex.ru", ""); got != "direct" {
		t.Errorf("mirror.yandex.ru via %s", got)
	}
	if got := route(t, c, "https://149.154.167.50/", "", "149.154.167.50"); got != "proxy" {
		t.Errorf("telegram IP via %s", got)
	}
	if code := c.AppTry(t, "curl -s -o /dev/null -w '%{http_code}' --max-time 20 https://www.youtube.com/"); code == "000" || code == "" {
		t.Errorf("youtube through the real tunnel failed: %q", code)
	}
	c.AppTry(t, "curl -s -o /dev/null --max-time 15 https://mirror.yandex.ru/")
	access := testenv.Sh(t, root, "cat /var/log/xray/access.log")
	if !strings.Contains(access, "www.youtube.com:443") {
		t.Errorf("server never saw youtube: the tunnel was bypassed\n%s", access)
	}
	if strings.Contains(access, "mirror.yandex.ru") {
		t.Errorf("mirror.yandex.ru reached the server: it must go direct")
	}
}

func TestTUNDNSNoLeak(t *testing.T) {
	c := tunClient(t)
	leak := func() string {
		c.Root(t, "(timeout 8 tcpdump -lni eth0 -A 'udp port 53' > /tmp/dns.txt 2>/dev/null &) ; sleep 1")
		c.AppTry(t, "getent hosts www.youtube.com; getent hosts ya.ru")
		c.Root(t, "sleep 8")
		return c.Root(t, "cat /tmp/dns.txt")
	}
	if strings.Contains(leak(), "youtube") {
		t.Fatal("DNS query for youtube visible on eth0 while connected")
	}
	c.App(t, "./wayveil disconnect")
	if !strings.Contains(leak(), "youtube") {
		t.Fatal("sanity: without the tunnel the query should be visible (test is not capturing)")
	}
}

func TestTUNAllMode(t *testing.T) {
	c := tunClient(t)
	c.App(t, "./wayveil mode all")
	if got := route(t, c, "https://mirror.yandex.ru/", "mirror.yandex.ru", ""); got != "proxy" {
		t.Errorf("mirror.yandex.ru via %s in all mode", got)
	}
}

func TestTUNUserDirectOverride(t *testing.T) {
	c := tunClient(t)
	c.App(t, "./wayveil rules direct youtube.com")
	if got := route(t, c, "https://www.youtube.com/", "www.youtube.com", ""); got != "direct" {
		t.Errorf("youtube via %s with direct override", got)
	}
}

func TestTUNKill9Recovers(t *testing.T) {
	c := tunClient(t)
	c.Root(t, "pkill -9 -x mihomo; sleep 1")
	if st := c.App(t, "./wayveil status"); !strings.HasPrefix(st, "failed") {
		t.Fatalf("status after kill: %q", st)
	}
	c.App(t, "./wayveil disconnect")
	if code := c.AppTry(t, "curl -s -o /dev/null -w '%{http_code}' --max-time 15 https://ya.ru/"); code == "000" || code == "" {
		t.Fatalf("internet broken after kill -9 + disconnect (code %q)\nip rule:\n%s", code, c.Root(t, "ip rule"))
	}
	c.App(t, "./wayveil connect")
	if st := c.App(t, "./wayveil status"); !strings.HasPrefix(st, "connected") {
		t.Fatalf("reconnect: %q", st)
	}
}
