//go:build integration

package server

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wayveil/wayveil/internal/remote"
	"github.com/wayveil/wayveil/internal/testenv"
)

var bg = context.Background()

func TestClientsCRUD(t *testing.T) {
	root, _, _ := installed(t, testenv.Start(t), "root", "wayveil")
	a, err := AddClient(bg, root, "Вася ноутбук")
	if err != nil || a.Name != "Вася ноутбук" || a.UUID == "" || a.Created.IsZero() {
		t.Fatalf("add returned %+v %v", a, err)
	}
	b, err := AddClient(bg, root, `O'Brien "x"`)
	if err != nil {
		t.Fatal(err)
	}
	list, err := ListClients(bg, root)
	if err != nil || len(list) != 2 || list[0].Name != "Вася ноутбук" || list[1].Name != `O'Brien "x"` {
		t.Fatalf("%+v %v", list, err)
	}
	if n := testenv.Sh(t, root, `jq '[.inbounds[]|select(.tag=="vless-in").settings.clients[]|select(.flow=="xtls-rprx-vision")]|length' /etc/xray/config.json`); n != "2" {
		t.Fatalf("clients in config: %s", n)
	}
	r, err := ReissueClient(bg, root, a.ID)
	if err != nil || r.UUID == a.UUID || r.ID != a.ID || r.Name != a.Name {
		t.Fatalf("reissue %+v %v", r, err)
	}
	if err := RemoveClient(bg, root, b.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ = ListClients(bg, root); len(list) != 1 {
		t.Fatalf("%+v", list)
	}
	var se *ScriptError
	if err := RemoveClient(bg, root, b.ID); !errors.As(err, &se) || se.Code != "no_such_client" {
		t.Fatalf("%v", err)
	}
	if s := testenv.Sh(t, root, "systemctl is-active xray"); s != "active" {
		t.Fatal(s)
	}
}

// clientConfig is an xray client config for the given link parameters.
func clientConfig(info ServerInfo, c Client) string {
	return fmt.Sprintf(`{"log":{"loglevel":"warning"},
"inbounds":[{"listen":"127.0.0.1","port":1080,"protocol":"socks","settings":{"udp":false}}],
"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"127.0.0.1","port":443,
  "users":[{"id":%q,"encryption":"none","flow":"xtls-rprx-vision"}]}]},
 "streamSettings":{"network":"tcp","security":"reality","realitySettings":{
  "serverName":%q,"fingerprint":"chrome","publicKey":%q,"shortId":%q}}}]}`,
		c.UUID, info.SNI, info.PublicKey, info.ShortID)
}

func TestTrafficSurvivesRestart(t *testing.T) {
	root, _, _ := installed(t, testenv.Start(t), "root", "wayveil")
	c, err := AddClient(bg, root, "phone")
	if err != nil {
		t.Fatal(err)
	}
	info, err := Info(bg, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.Run(bg, "cat > /tmp/client.json", []byte(clientConfig(info, c))); err != nil {
		t.Fatal(err)
	}
	testenv.Sh(t, root, "nohup /usr/local/bin/xray run -c /tmp/client.json >/tmp/client.log 2>&1 & sleep 2")
	code, err := root.Run(bg, "curl -sS -o /dev/null -w '%{http_code}' --max-time 20 -x socks5h://127.0.0.1:1080 https://www.microsoft.com/", nil)
	if err != nil || strings.TrimSpace(string(code)) == "000" {
		t.Fatalf("proxy request failed: %q %v\n%s", code, err, testenv.Sh(t, root, "cat /tmp/client.log; journalctl -u xray -n 10 --no-pager"))
	}
	tr, err := TrafficAll(bg, root)
	if err != nil || tr[c.ID].Down == 0 {
		t.Fatalf("traffic %+v %v", tr, err)
	}
	before := tr[c.ID].Down
	testenv.Sh(t, root, "systemctl restart xray")
	tr, _ = TrafficAll(bg, root)
	if tr[c.ID].Down < before {
		t.Fatalf("traffic dropped after restart: %d < %d", tr[c.ID].Down, before)
	}
	seen, err := LastSeen(bg, root)
	if err != nil || seen[c.ID].IP != "127.0.0.1" || time.Since(seen[c.ID].At) > 5*time.Minute {
		t.Fatalf("lastseen %+v %v", seen, err)
	}
}

func TestLastSeenSynthetic(t *testing.T) {
	root, _, _ := installed(t, testenv.Start(t), "root", "wayveil")
	lines := "2026/11/07 10:00:00.000001 from 1.1.1.1:5000 accepted tcp:a.com:443 [vless-in -> direct] email: c-aaaa0001\n" +
		"2026/11/07 11:00:00.000001 from tcp:2.2.2.2:5001 accepted tcp:a.com:443 [vless-in >> direct] email: c-aaaa0001\n"
	if _, err := root.Run(bg, "cat >> /var/log/xray/access.log", []byte(lines)); err != nil {
		t.Fatal(err)
	}
	seen, err := LastSeen(bg, root)
	want := time.Date(2026, 11, 7, 11, 0, 0, 0, time.UTC)
	if err != nil || seen["c-aaaa0001"].IP != "2.2.2.2" || !seen["c-aaaa0001"].At.Equal(want) {
		t.Fatalf("%+v %v", seen, err)
	}
}

func TestSetSNI(t *testing.T) {
	root, _, _ := installed(t, testenv.Start(t), "root", "wayveil")
	if err := SetSNI(bg, root, "www.apple.com"); err != nil {
		t.Fatal(err)
	}
	info, err := Info(bg, root)
	if err != nil || info.SNI != "www.apple.com" {
		t.Fatalf("%+v %v", info, err)
	}
	if s := testenv.Sh(t, root, "systemctl is-active xray"); s != "active" {
		t.Fatal(s)
	}
	_ = remote.ShellQuote
	_ = strconv.Itoa
}
