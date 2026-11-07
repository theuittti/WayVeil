// Package xrayclient builds the config of the local xray client: the "last
// hop" of the hybrid. mihomo (TUN, rules, DNS) hands proxied traffic to this
// password-protected socks inbound on 127.0.0.1, and xray carries it to the
// server over VLESS + REALITY + Vision — the server's own implementation, so
// the REALITY handshake always matches the server.
package xrayclient

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/theuittti/WayVeil/internal/link"
	"github.com/theuittti/WayVeil/internal/server"
)

// Version is pinned to the server's xray release.
const Version = server.XrayVersion

type Input struct {
	Server     link.Params
	ServerIP   string // dial address (resolved by the caller)
	SocksPort  int
	User, Pass string // random per run: other local users must not use the tunnel
	LogLevel   string // default "warning"
}

type (
	o = map[string]any
	a = []any
)

func Build(in Input) ([]byte, error) {
	if in.User == "" || in.Pass == "" {
		return nil, errors.New("xrayclient: socks credentials required")
	}
	if in.ServerIP == "" || in.SocksPort == 0 {
		return nil, errors.New("xrayclient: server IP and socks port required")
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
	lvl := in.LogLevel
	if lvl == "" {
		lvl = "warning"
	}
	cfg := o{
		"log": o{"loglevel": lvl},
		"inbounds": a{o{
			"tag": "socks-in", "listen": "127.0.0.1", "port": in.SocksPort, "protocol": "socks",
			"settings": o{"auth": "password", "accounts": a{o{"user": in.User, "pass": in.Pass}}, "udp": true, "ip": "127.0.0.1"},
		}},
		"outbounds": a{o{
			"tag": "proxy", "protocol": "vless",
			"settings": o{"vnext": a{o{"address": in.ServerIP, "port": port,
				"users": a{o{"id": s.UUID, "encryption": "none", "flow": "xtls-rprx-vision"}}}}},
			"streamSettings": o{"network": "tcp", "security": "reality",
				"realitySettings": o{"serverName": s.SNI, "fingerprint": fp, "publicKey": s.PublicKey, "shortId": s.ShortID}},
		}},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// Find locates the xray binary: $WAYVEIL_XRAY, next to the running
// executable, then <repo>/build/xray (development).
func Find() (string, error) {
	name := "xray"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	var cands []string
	if p := os.Getenv("WAYVEIL_XRAY"); p != "" {
		cands = append(cands, p)
	}
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(exe), name))
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		cands = append(cands, filepath.Join(filepath.Dir(file), "..", "..", "build", name))
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return filepath.Abs(c)
		}
	}
	return "", errors.New("xray binary not found (set WAYVEIL_XRAY or run make xray)")
}
