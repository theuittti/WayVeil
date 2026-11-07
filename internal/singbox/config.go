// Package singbox builds sing-box 1.14 configs for Wayveil's split tunnel.
package singbox

import (
	"encoding/json"
	"errors"
	"net/netip"

	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/link"
)

// Version is the pinned sing-box release the configs are written for.
const Version = "1.14.4"

type InboundKind int

const (
	TUN   InboundKind = iota // system-wide (needs CAP_NET_ADMIN)
	Mixed                    // socks/http on 127.0.0.1 (tests, debugging)
)

// RuleSetOrder is the order of proxied rule-sets; ru-blocked is required.
var RuleSetOrder = []string{"ru-blocked", "telegram", "meta", "twitter", "discord"}

type Input struct {
	Server                    link.Params
	ServerIPs                 []netip.Addr // resolved by the caller
	Mode                      clientstore.Mode
	AlwaysProxy, AlwaysDirect []string
	RuleSets                  map[string]string // tag → .srs path
	Inbound                   InboundKind
	MixedPort                 int
	ClashAddr, ClashSecret    string
	LogPath                   string
}

type (
	o = map[string]any
	a = []any
)

// Build returns the sing-box config (indented JSON).
func Build(in Input) ([]byte, error) {
	if len(in.ServerIPs) == 0 {
		return nil, errors.New("singbox: server IPs required")
	}
	smart := in.Mode != clientstore.ModeAll
	var sets []string
	for _, tag := range RuleSetOrder {
		if _, ok := in.RuleSets[tag]; ok {
			sets = append(sets, tag)
		}
	}
	if smart && (len(sets) == 0 || sets[0] != "ru-blocked") {
		return nil, errors.New("singbox: rule-set ru-blocked required in smart mode")
	}

	dial := in.ServerIPs[0]
	var cidrs []string
	for _, ip := range in.ServerIPs {
		cidrs = append(cidrs, netip.PrefixFrom(ip, ip.BitLen()).String())
		if ip.Is4() && !dial.Is4() {
			dial = ip
		}
	}
	serverRule := o{"ip_cidr": cidrs, "outbound": "direct"}
	if _, err := netip.ParseAddr(in.Server.Host); err != nil {
		serverRule["domain"] = []string{in.Server.Host} // never resolve/route the server through the tunnel
	}

	// ---- route ----
	rules := a{
		o{"action": "sniff"},
		o{"protocol": "dns", "action": "hijack-dns"},
		o{"ip_is_private": true, "outbound": "direct"},
		serverRule,
	}
	if len(in.AlwaysDirect) > 0 {
		rules = append(rules, o{"domain_suffix": in.AlwaysDirect, "outbound": "direct"})
	}
	final := "proxy"
	var ruleSetDefs a
	if smart {
		if len(in.AlwaysProxy) > 0 {
			rules = append(rules, o{"domain_suffix": in.AlwaysProxy, "outbound": "proxy"})
		}
		rules = append(rules, o{"rule_set": sets, "outbound": "proxy"})
		final = "direct"
		for _, tag := range sets {
			ruleSetDefs = append(ruleSetDefs, o{"tag": tag, "type": "local", "format": "binary", "path": in.RuleSets[tag]})
		}
	}
	route := o{"rules": rules, "final": final, "auto_detect_interface": true, "default_domain_resolver": "local"}
	if ruleSetDefs != nil {
		route["rule_set"] = ruleSetDefs
	}

	// ---- dns ----
	var dnsRules a
	if len(in.AlwaysDirect) > 0 {
		dnsRules = append(dnsRules, o{"domain_suffix": in.AlwaysDirect, "server": "local"})
	}
	dnsFinal := "remote"
	if smart {
		if len(in.AlwaysProxy) > 0 {
			dnsRules = append(dnsRules, o{"domain_suffix": in.AlwaysProxy, "server": "remote"})
		}
		dnsRules = append(dnsRules, o{"rule_set": sets, "server": "remote"})
		dnsFinal = "local"
	}
	dns := o{
		"servers": a{
			o{"tag": "remote", "type": "https", "server": "1.1.1.1", "detour": "proxy"},
			o{"tag": "local", "type": "https", "server": "77.88.8.8"},
		},
		"final":    dnsFinal,
		"strategy": "ipv4_only", // blocked sites must not bypass the tunnel over IPv6
	}
	if dnsRules != nil {
		dns["rules"] = dnsRules
	}

	// ---- inbound / outbounds ----
	var inbound o
	if in.Inbound == Mixed {
		inbound = o{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": in.MixedPort}
	} else {
		inbound = o{"type": "tun", "tag": "tun-in", "interface_name": "wayveil0",
			"address": []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
			"mtu":     1500, "auto_route": true, "strict_route": true, "stack": "mixed"}
	}
	s := in.Server
	fp := s.Fingerprint
	if fp == "" {
		fp = "chrome"
	}
	port := s.Port
	if port == 0 {
		port = 443
	}
	proxy := o{"type": "vless", "tag": "proxy", "server": dial.String(), "server_port": port,
		"uuid": s.UUID, "flow": "xtls-rprx-vision",
		"tls": o{"enabled": true, "server_name": s.SNI,
			"utls":    o{"enabled": true, "fingerprint": fp},
			"reality": o{"enabled": true, "public_key": s.PublicKey, "short_id": s.ShortID}}}

	logCfg := o{"level": "warn", "timestamp": true}
	if in.LogPath != "" {
		logCfg["output"] = in.LogPath
	}
	cfg := o{
		"log":          logCfg,
		"dns":          dns,
		"inbounds":     a{inbound},
		"outbounds":    a{proxy, o{"type": "direct", "tag": "direct"}},
		"route":        route,
		"experimental": o{"clash_api": o{"external_controller": in.ClashAddr, "secret": in.ClashSecret}},
	}
	return json.MarshalIndent(cfg, "", "  ")
}
