// Package mihomo builds mihomo (Clash.Meta) configs for Wayveil's split
// tunnel. The output is JSON, which mihomo reads as YAML.
package mihomo

import (
	"encoding/json"
	"errors"
	"net/netip"

	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/link"
	"github.com/theuittti/WayVeil/internal/lists"
	"github.com/theuittti/WayVeil/rules"
)

// Version is the pinned mihomo release the configs are written for.
const Version = "v1.19.33"

type InboundKind int

const (
	TUN   InboundKind = iota // system-wide (needs CAP_NET_ADMIN)
	Mixed                    // socks/http on 127.0.0.1 (tests, debugging)
)

const localDNS = "https://77.88.8.8/dns-query"

// localNames never get fake IPs and are resolved by the system resolver.
var localNames = []string{"+.lan", "+.local", "+.home.arpa", "+.localdomain", "localhost"}

var privateV4 = []string{"10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16"}
var privateV6 = []string{"::1/128", "fc00::/7", "fe80::/10"}

type Input struct {
	Server                    link.Params
	ServerIPs                 []netip.Addr // resolved by the caller
	Mode                      clientstore.Mode
	AlwaysProxy, AlwaysDirect []string
	ListsDir                  string // rule-set dir, relative to mihomo's home (-d)
	Inbound                   InboundKind
	MixedPort                 int
	ClashAddr, ClashSecret    string
	// Hybrid last hop: the local xray client's socks inbound.
	XrayPort           int
	XrayUser, XrayPass string
	LogLevel           string // default "warning"; "info" logs every routing decision
}

type (
	o = map[string]any
	a = []any
)

// Build returns the mihomo config.
func Build(in Input) ([]byte, error) {
	if len(in.ServerIPs) == 0 {
		return nil, errors.New("mihomo: server IPs required")
	}
	smart := in.Mode != clientstore.ModeAll

	var domainSets, ipSets []string
	providers := o{}
	for _, s := range rules.DefaultSources() {
		if s.Behavior == "ipcidr" {
			ipSets = append(ipSets, s.Tag)
		} else {
			domainSets = append(domainSets, s.Tag)
		}
		providers[s.Tag] = o{"type": "file", "behavior": s.Behavior, "format": "mrs", "path": lists.Path(in.ListsDir, s.Tag)}
	}

	// ---- rules ----
	var r []string
	for _, c := range privateV4 {
		r = append(r, "IP-CIDR,"+c+",DIRECT,no-resolve")
	}
	for _, c := range privateV6 {
		r = append(r, "IP-CIDR6,"+c+",DIRECT,no-resolve")
	}
	for _, ip := range in.ServerIPs { // never route the tunnel itself into the tunnel
		kind := "IP-CIDR6"
		if ip.Is4() {
			kind = "IP-CIDR"
		}
		r = append(r, kind+","+netip.PrefixFrom(ip, ip.BitLen()).String()+",DIRECT,no-resolve")
	}
	if _, err := netip.ParseAddr(in.Server.Host); err != nil {
		r = append(r, "DOMAIN,"+in.Server.Host+",DIRECT")
	}
	for _, d := range in.AlwaysDirect {
		r = append(r, "DOMAIN-SUFFIX,"+d+",DIRECT")
	}
	if smart {
		for _, d := range in.AlwaysProxy {
			r = append(r, "DOMAIN-SUFFIX,"+d+",proxy")
		}
		for _, t := range domainSets {
			r = append(r, "RULE-SET,"+t+",proxy")
		}
		for _, t := range ipSets {
			r = append(r, "RULE-SET,"+t+",proxy,no-resolve")
		}
		r = append(r, "MATCH,DIRECT")
	} else {
		r = append(r, "MATCH,proxy")
	}

	// ---- dns ----
	// fake-ip: apps get placeholder addresses, so a proxied domain is never
	// resolved locally (the server resolves it inside the tunnel) and direct
	// domains are resolved by mihomo over encrypted DoH. DNS "via #proxy" is
	// deliberately not used: it fails REALITY against xray >= 26.9.8.
	policy := o{}
	for _, n := range localNames { // LAN names keep working while connected
		policy[n] = []string{"system"}
	}
	dns := o{
		"enable": true, "ipv6": false, "enhanced-mode": "fake-ip", "fake-ip-range": "198.18.0.1/16",
		"fake-ip-filter":          localNames,
		"default-nameserver":      []string{"77.88.8.8"},
		"nameserver":              []string{localDNS},
		"proxy-server-nameserver": []string{localDNS},
		"nameserver-policy":       policy,
	}

	// ---- proxy: the local xray client (it speaks REALITY to the server) ----
	if in.XrayPort == 0 || in.XrayUser == "" || in.XrayPass == "" {
		return nil, errors.New("mihomo: xray socks port and credentials required")
	}
	proxy := o{"name": "xray", "type": "socks5", "server": "127.0.0.1", "port": in.XrayPort,
		"username": in.XrayUser, "password": in.XrayPass, "udp": true}
	var exclude []string
	for _, ip := range in.ServerIPs {
		exclude = append(exclude, netip.PrefixFrom(ip, ip.BitLen()).String())
	}

	logLevel := in.LogLevel
	if logLevel == "" {
		logLevel = "warning"
	}
	cfg := o{
		"mode": "rule", "log-level": logLevel, "ipv6": false, "allow-lan": false,
		"find-process-mode": "off", "geo-auto-update": false,
		"profile":             o{"store-selected": false, "store-fake-ip": false},
		"external-controller": in.ClashAddr, "secret": in.ClashSecret,
		"dns": dns, "proxies": a{proxy}, "rules": r,
		// Route by domain even when an app learned a real IP outside our DNS
		// (browser DoH, caches, hardcoded addresses).
		"sniffer": o{"enable": true, "sniff": o{
			"TLS":  o{"ports": []int{443}},
			"QUIC": o{"ports": []int{443}},
			"HTTP": o{"ports": []int{80}, "override-destination": true},
		}},
		"proxy-groups": a{o{"name": "proxy", "type": "select", "proxies": []string{"xray"}}},
	}
	if smart {
		cfg["rule-providers"] = providers
	}
	if in.Inbound == Mixed {
		cfg["mixed-port"] = in.MixedPort
		cfg["bind-address"] = "127.0.0.1"
	} else {
		cfg["tun"] = o{"enable": true, "device": "wayveil0", "stack": "mixed", "auto-route": true,
			"auto-detect-interface": true, "strict-route": true, "dns-hijack": []string{"any:53"}, "mtu": 1500,
			// xray's own connection to the server must never enter the TUN
			"route-exclude-address": exclude}
	}
	return json.MarshalIndent(cfg, "", "  ")
}
