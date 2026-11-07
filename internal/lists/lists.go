// Package lists keeps the sing-box rule-sets (blocked-in-RU domains and
// per-service IPs) fresh, never replacing a good file with a bad download.
package lists

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/theuittti/WayVeil/rules"
)

// DefaultSourcesURL lets source URLs change without an app release.
const DefaultSourcesURL = "https://raw.githubusercontent.com/theuittti/WayVeil/main/rules/sources.json"

const maxSize = 4 << 20

type Options struct {
	Dir        string // where .srs files live
	HTTP       *http.Client
	SourcesURL string // "" = DefaultSourcesURL
}

type UpdateResult struct {
	Updated, Kept []string
	Errors        map[string]string
}

// Valid reports whether b looks like a sing-box binary rule-set.
func Valid(b []byte) bool { return len(b) > 3 && string(b[:3]) == "SRS" && len(b) < maxSize }

func path(dir, tag string) string { return filepath.Join(dir, tag+".srs") }

func write(dir, tag string, b []byte) error {
	tmp := path(dir, tag) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path(dir, tag))
}

// Ensure returns tag → file path for every bundled source, writing the
// bundled copy wherever no valid cached file exists.
func Ensure(o Options) (map[string]string, error) {
	if err := os.MkdirAll(o.Dir, 0o700); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, s := range rules.DefaultSources() {
		p := path(o.Dir, s.Tag)
		if b, err := os.ReadFile(p); err != nil || !Valid(b) {
			bb, ok := rules.Bundled(s.Tag)
			if !ok {
				return nil, fmt.Errorf("no bundled copy of %s", s.Tag)
			}
			if err := write(o.Dir, s.Tag, bb); err != nil {
				return nil, err
			}
		}
		out[s.Tag] = p
	}
	return out, nil
}

func (o Options) client() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func get(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
}

// sources fetches the remote source list, falling back to the bundled one.
func (o Options) sources(ctx context.Context) []rules.Source {
	u := o.SourcesURL
	if u == "" {
		u = DefaultSourcesURL
	}
	if b, err := get(ctx, o.client(), u); err == nil {
		var s []rules.Source
		if json.Unmarshal(b, &s) == nil && len(s) > 0 {
			return s
		}
	}
	return rules.DefaultSources()
}

// Update downloads every source; a file is replaced only by a valid download.
func Update(ctx context.Context, o Options) (UpdateResult, error) {
	res := UpdateResult{Errors: map[string]string{}}
	if _, err := Ensure(o); err != nil {
		return res, err
	}
	for _, s := range o.sources(ctx) {
		b, err := get(ctx, o.client(), s.URL)
		switch {
		case err != nil:
			res.Errors[s.Tag] = err.Error()
		case !Valid(b):
			res.Errors[s.Tag] = "not a sing-box rule-set"
		default:
			if err := write(o.Dir, s.Tag, b); err != nil {
				res.Errors[s.Tag] = err.Error()
			} else {
				res.Updated = append(res.Updated, s.Tag)
				continue
			}
		}
		res.Kept = append(res.Kept, s.Tag)
	}
	return res, nil
}
