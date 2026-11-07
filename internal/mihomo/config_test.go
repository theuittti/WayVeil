package mihomo

import (
	"encoding/json"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/link"
	"github.com/theuittti/WayVeil/internal/lists"
)

func input(t *testing.T) Input {
	return Input{
		Server:    link.Params{UUID: "11111111-2222-3333-4444-555555555555", Host: "198.51.100.7", Port: 443, SNI: "www.nu.nl", PublicKey: "KjRhl1CVm-XAfrJdTiWyC3zui05Tcg2bMNRBZOH0CUM", ShortID: "9e5ee700", Fingerprint: "chrome", Flow: "xtls-rprx-vision"},
		ServerIPs: []netip.Addr{netip.MustParseAddr("198.51.100.7")},
		Mode:      clientstore.ModeSmart,
		ListsDir:  "lists",
		Inbound:   TUN,
		ClashAddr: "127.0.0.1:39090", ClashSecret: "s3cr3t",
		XrayPort: 10808, XrayUser: "wv", XrayPass: "p4ss",
	}
}

type obj = map[string]any

func build(t *testing.T, in Input) obj {
	t.Helper()
	b, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	var m obj
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func ruleList(m obj) []string {
	var out []string
	for _, r := range m["rules"].([]any) {
		out = append(out, r.(string))
	}
	return out
}

// shape reduces a rule to its type and target: "IP-CIDR>DIRECT", "RULE-SET:ru-blocked>proxy".
func shape(r string) string {
	f := strings.Split(r, ",")
	switch f[0] {
	case "RULE-SET":
		return "RULE-SET:" + f[1] + ">" + f[2]
	case "MATCH":
		return "MATCH>" + f[1]
	}
	return f[0] + ">" + f[2]
}

func TestSmartRuleOrder(t *testing.T) {
	in := input(t)
	in.AlwaysDirect = []string{"youtube.com"}
	in.AlwaysProxy = []string{"example.org"}
	r := ruleList(build(t, in))
	var shapes []string
	for _, x := range r {
		s := shape(x)
		if len(shapes) == 0 || shapes[len(shapes)-1] != s {
			shapes = append(shapes, s)
		}
	}
	want := []string{"IP-CIDR>DIRECT", "IP-CIDR6>DIRECT", "IP-CIDR>DIRECT", "DOMAIN-SUFFIX>DIRECT", "DOMAIN-SUFFIX>proxy",
		"RULE-SET:ru-blocked>proxy", "RULE-SET:telegram-domain>proxy", "RULE-SET:meta-domain>proxy", "RULE-SET:twitter-domain>proxy", "RULE-SET:discord-domain>proxy",
		"RULE-SET:telegram-ip>proxy", "RULE-SET:meta-ip>proxy", "RULE-SET:twitter-ip>proxy", "RULE-SET:discord-ip>proxy", "MATCH>DIRECT"}
	if !reflect.DeepEqual(shapes, want) {
		t.Fatalf("order:\n%v", strings.Join(shapes, "\n"))
	}
	for _, x := range r {
		if strings.HasPrefix(x, "RULE-SET,") && strings.Contains(x, "-ip,") && !strings.HasSuffix(x, ",no-resolve") {
			t.Fatalf("ip rule-set must be no-resolve: %s", x)
		}
	}
	if m := build(t, in)["mode"]; m != "rule" {
		t.Fatal(m)
	}
}

func TestAllMode(t *testing.T) {
	in := input(t)
	in.Mode = clientstore.ModeAll
	in.AlwaysDirect = []string{"gosuslugi.ru"}
	m := build(t, in)
	r := ruleList(m)
	if r[len(r)-1] != "MATCH,proxy" || strings.Contains(strings.Join(r, "|"), "RULE-SET") {
		t.Fatalf("%v", r)
	}
	if !contains(r, "DOMAIN-SUFFIX,gosuslugi.ru,DIRECT") {
		t.Fatal("always-direct missing in all mode")
	}
	if _, ok := m["rule-providers"]; ok {
		t.Fatal("no rule-providers needed in all mode")
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func strs(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestServerExcluded(t *testing.T) {
	in := input(t)
	in.Server.Host = "vpn.example.com"
	in.ServerIPs = []netip.Addr{netip.MustParseAddr("2001:db8::7"), netip.MustParseAddr("198.51.100.7")}
	m := build(t, in)
	r := ruleList(m)
	for _, want := range []string{"IP-CIDR6,2001:db8::7/128,DIRECT,no-resolve", "IP-CIDR,198.51.100.7/32,DIRECT,no-resolve", "DOMAIN,vpn.example.com,DIRECT"} {
		i := indexOf(r, want)
		if i < 0 || i > indexOf(r, "RULE-SET,ru-blocked,proxy") {
			t.Fatalf("%s missing or after proxy rules: %v", want, r)
		}
	}
	// Hybrid: proxied traffic goes to the local xray client (socks5), which
	// carries it to the server; the server's IPs never enter the TUN.
	p := m["proxies"].([]any)[0].(obj)
	g := m["proxy-groups"].([]any)[0].(obj)
	if g["name"] != "proxy" || g["type"] != "select" || !reflect.DeepEqual(strs(g["proxies"]), []string{"xray"}) {
		t.Fatalf("group %v", g)
	}
	if p["name"] != "xray" || p["type"] != "socks5" || p["server"] != "127.0.0.1" || p["port"] != float64(10808) ||
		p["username"] != "wv" || p["password"] != "p4ss" || p["udp"] != true {
		t.Fatalf("proxy %v", p)
	}
	if ex := strs(m["tun"].(obj)["route-exclude-address"]); !reflect.DeepEqual(ex, []string{"2001:db8::7/128", "198.51.100.7/32"}) {
		t.Fatalf("route-exclude-address %v", ex)
	}
}

func indexOf(l []string, s string) int {
	for i, x := range l {
		if x == s {
			return i
		}
	}
	return -1
}

// fake-ip: proxied domains are never resolved locally (the server resolves
// them inside the tunnel); direct ones go to encrypted DoH. mihomo's DNS
// "#proxy" path fails REALITY against xray >= 26.9.8, so it must not be used.
func TestDNSNoLeak(t *testing.T) {
	for _, mode := range []clientstore.Mode{clientstore.ModeSmart, clientstore.ModeAll} {
		in := input(t)
		in.Mode = mode
		in.AlwaysProxy = []string{"example.org"}
		in.AlwaysDirect = []string{"youtube.com"}
		m := build(t, in)
		d := m["dns"].(obj)
		if d["enable"] != true || d["ipv6"] != false || d["enhanced-mode"] != "fake-ip" || d["fake-ip-range"] != "198.18.0.1/16" {
			t.Fatalf("%s: %v", mode, d)
		}
		if ns := strs(d["nameserver"]); !reflect.DeepEqual(ns, []string{"https://77.88.8.8/dns-query"}) {
			t.Fatalf("%s: nameserver %v", mode, ns)
		}
		b, _ := Build(in)
		if strings.Contains(string(b), "#proxy") {
			t.Fatalf("%s: DNS through the proxy is broken with REALITY+MLKEM, must not be used", mode)
		}
		if !reflect.DeepEqual(strs(m["tun"].(obj)["dns-hijack"]), []string{"any:53"}) {
			t.Fatal("dns-hijack")
		}
	}
}

func TestTUNShape(t *testing.T) {
	m := build(t, input(t))
	tun := m["tun"].(obj)
	if tun["enable"] != true || tun["device"] != "wayveil0" || tun["auto-route"] != true || tun["strict-route"] != true ||
		tun["auto-detect-interface"] != true || tun["stack"] != "mixed" {
		t.Fatalf("%v", tun)
	}
	if m["ipv6"] != false {
		t.Fatal("ipv6 must be off: blocked sites must not bypass the tunnel over IPv6")
	}
	mx := input(t)
	mx.Inbound, mx.MixedPort = Mixed, 21080
	m = build(t, mx)
	if _, ok := m["tun"]; ok || m["mixed-port"] != float64(21080) || m["bind-address"] != "127.0.0.1" || m["allow-lan"] != false {
		t.Fatalf("mixed %v", m)
	}
}

func TestClashLocalOnly(t *testing.T) {
	m := build(t, input(t))
	if !strings.HasPrefix(m["external-controller"].(string), "127.0.0.1:") || m["secret"] != "s3cr3t" {
		t.Fatalf("%v", m)
	}
}

func TestRuleProviders(t *testing.T) {
	rp := build(t, input(t))["rule-providers"].(obj)
	ru := rp["ru-blocked"].(obj)
	tg := rp["telegram-ip"].(obj)
	if ru["type"] != "file" || ru["format"] != "mrs" || ru["behavior"] != "domain" || ru["path"] != "lists/ru-blocked.mrs" || tg["behavior"] != "ipcidr" {
		t.Fatalf("%v", rp)
	}
}

func TestMissingInputs(t *testing.T) {
	in := input(t)
	in.ServerIPs = nil
	if _, err := Build(in); err == nil {
		t.Fatal("missing server IPs must fail")
	}
}

func TestMihomoCheck(t *testing.T) {
	bin, err := Find()
	if err != nil {
		t.Skip("run make mihomo: ", err)
	}
	home := t.TempDir()
	if _, err := lists.Ensure(lists.Options{Dir: filepath.Join(home, "lists")}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []clientstore.Mode{clientstore.ModeSmart, clientstore.ModeAll} {
		for _, ib := range []InboundKind{TUN, Mixed} {
			in := input(t)
			in.Mode, in.Inbound, in.MixedPort = mode, ib, 21080
			in.AlwaysDirect, in.AlwaysProxy = []string{"youtube.com"}, []string{"example.org"}
			b, err := Build(in)
			if err != nil {
				t.Fatal(err)
			}
			f := filepath.Join(home, "config.yaml")
			os.WriteFile(f, b, 0o600)
			if out, err := exec.Command(bin, "-t", "-d", home, "-f", f).CombinedOutput(); err != nil {
				t.Fatalf("%s/%d: %v\n%s", mode, ib, err, out)
			}
		}
	}
}
