package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/theuittti/WayVeil/internal/link"
	"github.com/theuittti/WayVeil/internal/remote"
)

type Client struct {
	ID      string    `json:"id"` // xray email, "c-" + 8 hex
	UUID    string    `json:"uuid"`
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
}

type ServerInfo struct {
	Address     string `json:"address"`
	SNI         string `json:"sni"`
	PublicKey   string `json:"publicKey"`
	ShortID     string `json:"shortId"`
	XrayVersion string `json:"xrayVersion"`
}

type Traffic struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

type Seen struct {
	IP string    `json:"ip"`
	At time.Time `json:"at"`
}

// ctl runs the embedded ctl.sh subcommand and decodes its ##RESULT into v.
func ctl(ctx context.Context, r remote.Runner, v any, args ...string) error {
	res, err := runScript(ctx, r, "ctl", nil, nil, nil, args...)
	if err != nil {
		return err
	}
	if v == nil {
		return nil
	}
	if len(res) == 0 {
		return errors.New("ctl " + args[0] + ": no result")
	}
	return json.Unmarshal(res, v)
}

func Info(ctx context.Context, r remote.Runner) (ServerInfo, error) {
	var i ServerInfo
	return i, ctl(ctx, r, &i, "info")
}

func ListClients(ctx context.Context, r remote.Runner) ([]Client, error) {
	var out struct{ Clients []Client }
	if err := ctl(ctx, r, &out, "list"); err != nil {
		return nil, err
	}
	sort.SliceStable(out.Clients, func(i, j int) bool { return out.Clients[i].Created.Before(out.Clients[j].Created) })
	return out.Clients, nil
}

func AddClient(ctx context.Context, r remote.Runner, name string) (Client, error) {
	var out struct{ Client Client }
	err := ctl(ctx, r, &out, "add", base64.StdEncoding.EncodeToString([]byte(name)))
	return out.Client, err
}

func RemoveClient(ctx context.Context, r remote.Runner, id string) error {
	return ctl(ctx, r, nil, "remove", id)
}

// ReissueClient gives the client a new UUID (old link stops working); ID,
// name and traffic are kept.
func ReissueClient(ctx context.Context, r remote.Runner, id string) (Client, error) {
	var out struct{ Client Client }
	err := ctl(ctx, r, &out, "reissue", id)
	return out.Client, err
}

// TrafficAll returns accumulated traffic per client ID; it survives xray restarts.
func TrafficAll(ctx context.Context, r remote.Runner) (map[string]Traffic, error) {
	var out struct{ Traffic map[string]Traffic }
	return out.Traffic, ctl(ctx, r, &out, "stats")
}

func LastSeen(ctx context.Context, r remote.Runner) (map[string]Seen, error) {
	var out struct {
		LastSeen map[string]Seen `json:"lastSeen"`
	}
	return out.LastSeen, ctl(ctx, r, &out, "lastseen")
}

// SetSNI changes the REALITY target; every client link changes.
func SetSNI(ctx context.Context, r remote.Runner, domain string) error {
	return ctl(ctx, r, nil, "set-sni", domain)
}

func ClientLink(info ServerInfo, c Client) string {
	return link.Build(link.Params{UUID: c.UUID, Host: info.Address, SNI: info.SNI,
		PublicKey: info.PublicKey, ShortID: info.ShortID, Name: c.Name})
}
