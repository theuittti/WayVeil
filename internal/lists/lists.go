// Package lists keeps the mihomo rule-sets (blocked-in-RU domains and
// per-service IPs) fresh, never replacing a good file with a bad download.
package lists

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/theuittti/WayVeil/internal/appdir"
	"github.com/theuittti/WayVeil/rules"
)

// DefaultSourcesURL lets source URLs change without an app release.
const DefaultSourcesURL = "https://raw.githubusercontent.com/theuittti/WayVeil/main/rules/sources.json"

const maxSize = 4 << 20

type Options struct {
	Dir        string // where .mrs files live
	HTTP       *http.Client
	SourcesURL string // "" = DefaultSourcesURL
}

type UpdateResult struct {
	Updated, Kept []string
	Errors        map[string]string
}

// DefaultDir is where the client keeps its rule-sets (CacheDir()/lists).
func DefaultDir() (string, error) {
	c, err := appdir.CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(c, "lists"), nil
}

// Valid reports whether b is a complete mihomo .mrs rule-set of the given
// behavior ("domain" | "ipcidr"): the zstd stream must decode fully and start
// with the MRS v1 header. A truncated file keeps the zstd magic but fails here;
// mihomo would otherwise accept it and silently route blocked sites direct.
func Valid(b []byte, behavior string) bool {
	if len(b) < 4 || len(b) >= maxSize || b[0] != 0x28 || b[1] != 0xb5 || b[2] != 0x2f || b[3] != 0xfd {
		return false
	}
	d, err := zstd.NewReader(bytes.NewReader(b), zstd.WithDecoderMaxMemory(64<<20))
	if err != nil {
		return false
	}
	defer d.Close()
	out, err := io.ReadAll(io.LimitReader(d, 64<<20))
	if err != nil || len(out) < 5 || string(out[:4]) != "MRS\x01" {
		return false
	}
	want := byte(0)
	if behavior == "ipcidr" {
		want = 1
	}
	return out[4] == want
}

// Path is where the rule-set with tag lives inside dir.
func Path(dir, tag string) string { return filepath.Join(dir, tag+".mrs") }

func path(dir, tag string) string { return Path(dir, tag) }

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
		if b, err := os.ReadFile(p); err != nil || !Valid(b, s.Behavior) {
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

// sources returns the bundled sources, with download URLs overridden by the
// remote copy on main — only for known tags and only https URLs, so a bad
// remote file can neither invent paths nor downgrade to plain http.
func (o Options) sources(ctx context.Context) []rules.Source {
	src := rules.DefaultSources()
	u := o.SourcesURL
	if u == "" {
		u = DefaultSourcesURL
	}
	b, err := get(ctx, o.client(), u)
	if err != nil {
		return src
	}
	var remote []rules.Source
	if json.Unmarshal(b, &remote) != nil {
		return src
	}
	for i := range src {
		for _, r := range remote {
			if r.Tag == src[i].Tag && strings.HasPrefix(r.URL, "https://") {
				src[i].URL = r.URL
			}
		}
	}
	return src
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
		case !Valid(b, s.Behavior):
			res.Errors[s.Tag] = "not a mihomo rule-set"
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
