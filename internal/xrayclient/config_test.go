package xrayclient

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/theuittti/WayVeil/internal/link"
	"github.com/theuittti/WayVeil/internal/server"
)

func input() Input {
	return Input{
		Server:    link.Params{UUID: "11111111-2222-3333-4444-555555555555", Host: "198.51.100.7", Port: 443, SNI: "www.nu.nl", PublicKey: "KjRhl1CVm-XAfrJdTiWyC3zui05Tcg2bMNRBZOH0CUM", ShortID: "9e5ee700", Fingerprint: "chrome"},
		ServerIP:  "198.51.100.7",
		SocksPort: 10808, User: "wv", Pass: "p4ss",
	}
}

type obj = map[string]any

func TestConfigShape(t *testing.T) {
	b, err := Build(input())
	if err != nil {
		t.Fatal(err)
	}
	var m obj
	json.Unmarshal(b, &m)
	in := m["inbounds"].([]any)[0].(obj)
	st := in["settings"].(obj)
	acc := st["accounts"].([]any)[0].(obj)
	if in["listen"] != "127.0.0.1" || in["port"] != float64(10808) || in["protocol"] != "socks" ||
		st["auth"] != "password" || st["udp"] != true || acc["user"] != "wv" || acc["pass"] != "p4ss" {
		t.Fatalf("inbound %v", in)
	}
	out := m["outbounds"].([]any)[0].(obj)
	vn := out["settings"].(obj)["vnext"].([]any)[0].(obj)
	user := vn["users"].([]any)[0].(obj)
	rs := out["streamSettings"].(obj)["realitySettings"].(obj)
	if out["protocol"] != "vless" || vn["address"] != "198.51.100.7" || vn["port"] != float64(443) ||
		user["id"] != input().Server.UUID || user["flow"] != "xtls-rprx-vision" || user["encryption"] != "none" ||
		rs["serverName"] != "www.nu.nl" || rs["publicKey"] != input().Server.PublicKey || rs["shortId"] != "9e5ee700" || rs["fingerprint"] != "chrome" {
		t.Fatalf("outbound %v", out)
	}
}

func TestRequiresCredentials(t *testing.T) {
	in := input()
	in.Pass = ""
	if _, err := Build(in); err == nil {
		t.Fatal("socks must be password-protected (other local users)")
	}
}

func TestPinnedToServerVersion(t *testing.T) {
	if Version != server.XrayVersion {
		t.Fatalf("client %s != server %s", Version, server.XrayVersion)
	}
}

func TestXrayAcceptsConfig(t *testing.T) {
	bin, err := Find()
	if err != nil {
		t.Skip("run make xray: ", err)
	}
	b, _ := Build(input())
	f := filepath.Join(t.TempDir(), "xray.json")
	os.WriteFile(f, b, 0o600)
	if out, err := exec.Command(bin, "run", "-test", "-c", f).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
