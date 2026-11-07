package singbox

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

func ruleSets(t *testing.T) map[string]string {
	t.Helper()
	p, err := lists.Ensure(lists.Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func input(t *testing.T) Input {
	return Input{
		Server:    link.Params{UUID: "11111111-2222-3333-4444-555555555555", Host: "198.51.100.7", Port: 443, SNI: "www.nu.nl", PublicKey: "KjRhl1CVm-XAfrJdTiWyC3zui05Tcg2bMNRBZOH0CUM", ShortID: "9e5ee700", Fingerprint: "chrome", Flow: "xtls-rprx-vision"},
		ServerIPs: []netip.Addr{netip.MustParseAddr("198.51.100.7")},
		Mode:      clientstore.ModeSmart,
		RuleSets:  ruleSets(t),
		Inbound:   TUN,
		ClashAddr: "127.0.0.1:39090", ClashSecret: "s3cr3t",
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

func rules(m obj) []obj {
	var out []obj
	for _, r := range m["route"].(obj)["rules"].([]any) {
		out = append(out, r.(obj))
	}
	return out
}

func strs(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

// kind describes a route rule compactly: "sniff", "hijack-dns", "private>direct", ...
func kind(r obj) string {
	switch {
	case r["action"] == "sniff":
		return "sniff"
	case r["action"] == "hijack-dns":
		return "hijack-dns"
	case r["ip_is_private"] == true:
		return "private>" + r["outbound"].(string)
	case r["rule_set"] != nil:
		return "ruleset>" + r["outbound"].(string)
	case r["ip_cidr"] != nil:
		return "server>" + r["outbound"].(string)
	case r["domain_suffix"] != nil:
		return "user>" + r["outbound"].(string)
	}
	return "?"
}

func kinds(m obj) []string {
	var k []string
	for _, r := range rules(m) {
		k = append(k, kind(r))
	}
	return k
}

func TestSmartRouteOrder(t *testing.T) {
	in := input(t)
	in.AlwaysDirect = []string{"youtube.com"}
	in.AlwaysProxy = []string{"example.org"}
	m := build(t, in)
	want := []string{"sniff", "hijack-dns", "private>direct", "server>direct", "user>direct", "user>proxy", "ruleset>proxy"}
	if got := kinds(m); !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v", got)
	}
	if m["route"].(obj)["final"] != "direct" {
		t.Fatal("final")
	}
	rs := strs(rules(m)[6]["rule_set"])
	if !reflect.DeepEqual(rs, []string{"ru-blocked", "telegram", "meta", "twitter", "discord"}) {
		t.Fatalf("rule sets %v", rs)
	}
}

func TestAllMode(t *testing.T) {
	in := input(t)
	in.Mode = clientstore.ModeAll
	in.AlwaysDirect = []string{"gosuslugi.ru"}
	m := build(t, in)
	want := []string{"sniff", "hijack-dns", "private>direct", "server>direct", "user>direct"}
	if got := kinds(m); !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v", got)
	}
	if m["route"].(obj)["final"] != "proxy" || m["dns"].(obj)["final"] != "remote" {
		t.Fatal("finals")
	}
}

func TestServerExcluded(t *testing.T) {
	in := input(t)
	in.Server.Host = "vpn.example.com"
	in.ServerIPs = []netip.Addr{netip.MustParseAddr("2001:db8::7"), netip.MustParseAddr("198.51.100.7")}
	m := build(t, in)
	r := rules(m)[3]
	if r["outbound"] != "direct" || !reflect.DeepEqual(strs(r["ip_cidr"]), []string{"2001:db8::7/128", "198.51.100.7/32"}) ||
		!reflect.DeepEqual(strs(r["domain"]), []string{"vpn.example.com"}) {
		t.Fatalf("server rule %v", r)
	}
	var proxy obj
	for _, o := range m["outbounds"].([]any) {
		if o.(obj)["tag"] == "proxy" {
			proxy = o.(obj)
		}
	}
	if proxy["server"] != "198.51.100.7" {
		t.Fatalf("must dial an IPv4 address, got %v", proxy["server"])
	}
	tls := proxy["tls"].(obj)
	if tls["server_name"] != "www.nu.nl" || tls["reality"].(obj)["public_key"] != in.Server.PublicKey ||
		tls["utls"].(obj)["fingerprint"] != "chrome" || proxy["flow"] != "xtls-rprx-vision" {
		t.Fatalf("outbound %v", proxy)
	}
}

func TestUserRules(t *testing.T) {
	in := input(t)
	in.AlwaysDirect = []string{"youtube.com"}
	in.AlwaysProxy = []string{"example.org"}
	m := build(t, in)
	if got := strs(rules(m)[4]["domain_suffix"]); !reflect.DeepEqual(got, []string{"youtube.com"}) {
		t.Fatal(got)
	}
	dnsRules := m["dns"].(obj)["rules"].([]any)
	var toRemote, toLocal []string
	for _, r := range dnsRules {
		r := r.(obj)
		if ds, ok := r["domain_suffix"]; ok {
			if r["server"] == "remote" {
				toRemote = append(toRemote, strs(ds)...)
			} else {
				toLocal = append(toLocal, strs(ds)...)
			}
		}
	}
	if !reflect.DeepEqual(toRemote, []string{"example.org"}) || !reflect.DeepEqual(toLocal, []string{"youtube.com"}) {
		t.Fatalf("dns remote %v local %v", toRemote, toLocal)
	}
}

func TestDNSNoLeak(t *testing.T) {
	m := build(t, input(t))
	d := m["dns"].(obj)
	servers := map[string]obj{}
	for _, s := range d["servers"].([]any) {
		servers[s.(obj)["tag"].(string)] = s.(obj)
	}
	if r := servers["remote"]; r["type"] != "https" || r["server"] != "1.1.1.1" || r["detour"] != "proxy" {
		t.Fatalf("remote %v", r)
	}
	if l := servers["local"]; l["type"] != "https" || l["server"] != "77.88.8.8" {
		t.Fatalf("local %v", l)
	}
	if d["strategy"] != "ipv4_only" || d["final"] != "local" {
		t.Fatalf("dns %v", d)
	}
	if !strings.Contains(strings.Join(kinds(m), ","), "hijack-dns") {
		t.Fatal("no hijack-dns")
	}
}

func TestTUNShape(t *testing.T) {
	m := build(t, input(t))
	in := m["inbounds"].([]any)[0].(obj)
	if in["type"] != "tun" || in["interface_name"] != "wayveil0" || in["auto_route"] != true || in["strict_route"] != true ||
		!reflect.DeepEqual(strs(in["address"]), []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"}) {
		t.Fatalf("%v", in)
	}
	mx := input(t)
	mx.Inbound, mx.MixedPort = Mixed, 21080
	in = build(t, mx)["inbounds"].([]any)[0].(obj)
	if in["type"] != "mixed" || in["listen"] != "127.0.0.1" || in["listen_port"] != float64(21080) {
		t.Fatalf("%v", in)
	}
}

func TestClashLocalOnly(t *testing.T) {
	c := build(t, input(t))["experimental"].(obj)["clash_api"].(obj)
	if !strings.HasPrefix(c["external_controller"].(string), "127.0.0.1:") || c["secret"] != "s3cr3t" {
		t.Fatalf("%v", c)
	}
}

func TestMissingInputs(t *testing.T) {
	in := input(t)
	delete(in.RuleSets, "ru-blocked")
	if _, err := Build(in); err == nil {
		t.Fatal("missing ru-blocked must fail")
	}
	in = input(t)
	in.ServerIPs = nil
	if _, err := Build(in); err == nil {
		t.Fatal("missing server IPs must fail")
	}
}

func TestSingBoxCheck(t *testing.T) {
	bin, err := Find()
	if err != nil {
		t.Skip("run make singbox: ", err)
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
			f := filepath.Join(t.TempDir(), "c.json")
			os.WriteFile(f, b, 0o600)
			if out, err := exec.Command(bin, "check", "-c", f).CombinedOutput(); err != nil {
				t.Fatalf("%s/%d: %v\n%s", mode, ib, err, out)
			}
		}
	}
}
