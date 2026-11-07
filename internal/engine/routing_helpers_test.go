//go:build integration || e2e

package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/theuittti/WayVeil/internal/testenv"
)

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

