package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Clash talks to mihomo's Clash-compatible API on 127.0.0.1.
type Clash struct {
	Addr, Secret string
	HTTP         *http.Client
}

type Conn struct {
	Host   string
	DestIP string
	Chains []string
}

type Traffic struct{ Up, Down int64 }

func (c Clash) get(ctx context.Context, path string, v any) error {
	cl := c.HTTP
	if cl == nil {
		cl = &http.Client{Timeout: 3 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+c.Addr+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Secret)
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("clash api %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (c Clash) Version(ctx context.Context) error {
	var v map[string]any
	return c.get(ctx, "/version", &v)
}

func (c Clash) Connections(ctx context.Context) ([]Conn, Traffic, error) {
	var r struct {
		DownloadTotal int64 `json:"downloadTotal"`
		UploadTotal   int64 `json:"uploadTotal"`
		Connections   []struct {
			Metadata struct {
				Host          string `json:"host"`
				DestinationIP string `json:"destinationIP"`
			} `json:"metadata"`
			Chains []string `json:"chains"`
		} `json:"connections"`
	}
	if err := c.get(ctx, "/connections", &r); err != nil {
		return nil, Traffic{}, err
	}
	out := make([]Conn, 0, len(r.Connections))
	for _, x := range r.Connections {
		out = append(out, Conn{Host: x.Metadata.Host, DestIP: x.Metadata.DestinationIP, Chains: x.Chains})
	}
	return out, Traffic{Up: r.UploadTotal, Down: r.DownloadTotal}, nil
}
