// Package clientstore keeps the client's settings: connection profile,
// routing mode and the user's own domain rules.
package clientstore

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/idna"

	"github.com/theuittti/WayVeil/internal/appdir"
	"github.com/theuittti/WayVeil/internal/profile"
)

type Mode string

const (
	ModeSmart Mode = "smart" // blocked-in-RU via proxy, rest direct
	ModeAll   Mode = "all"   // everything via proxy
)

type Settings struct {
	Mode         Mode             `json:"mode"`
	AlwaysProxy  []string         `json:"alwaysProxy"`
	AlwaysDirect []string         `json:"alwaysDirect"`
	Profile      *profile.Profile `json:"profile,omitempty"`
	ListsUpdated time.Time        `json:"listsUpdated"`
}

var ErrBadDomain = errors.New("bad_domain")

func file() (string, error) {
	d, err := appdir.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "settings.json"), nil
}

// Load returns saved settings, or defaults when none are saved yet.
func Load() (Settings, error) {
	s := Settings{Mode: ModeSmart, AlwaysProxy: []string{}, AlwaysDirect: []string{}}
	f, err := file()
	if err != nil {
		return s, err
	}
	b, err := os.ReadFile(f)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	if s.Mode != ModeAll {
		s.Mode = ModeSmart
	}
	if s.AlwaysProxy == nil {
		s.AlwaysProxy = []string{}
	}
	if s.AlwaysDirect == nil {
		s.AlwaysDirect = []string{}
	}
	return s, nil
}

func Save(s Settings) error {
	f, err := file()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f)
}

// NormalizeDomain turns user input such as "https://Example.COM/x" or
// "*.site.ru" into a bare lower-case ASCII domain.
func NormalizeDomain(in string) (string, error) {
	s := strings.TrimSpace(in)
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", ErrBadDomain
		}
		s = u.Host
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimPrefix(s, "*.")
	s = strings.TrimSuffix(strings.ToLower(s), ".")
	if s == "" || !strings.Contains(s, ".") || strings.ContainsAny(s, " \t") {
		return "", ErrBadDomain
	}
	a, err := idna.Lookup.ToASCII(s)
	if err != nil {
		return "", ErrBadDomain
	}
	return a, nil
}

// AddRule normalises domain and adds it to list (once), removing it from
// the opposite list so a domain is never both "always proxy" and "always direct".
func (s *Settings) AddRule(list *[]string, domain string) error {
	d, err := NormalizeDomain(domain)
	if err != nil {
		return err
	}
	other := &s.AlwaysDirect
	if list == &s.AlwaysDirect {
		other = &s.AlwaysProxy
	}
	*other = remove(*other, d)
	for _, x := range *list {
		if x == d {
			return nil
		}
	}
	*list = append(*list, d)
	return nil
}

// RemoveRule drops domain from both lists.
func (s *Settings) RemoveRule(domain string) error {
	d, err := NormalizeDomain(domain)
	if err != nil {
		return err
	}
	s.AlwaysProxy = remove(s.AlwaysProxy, d)
	s.AlwaysDirect = remove(s.AlwaysDirect, d)
	return nil
}

func remove(list []string, d string) []string {
	out := list[:0:0]
	for _, x := range list {
		if x != d {
			out = append(out, x)
		}
	}
	return out
}
