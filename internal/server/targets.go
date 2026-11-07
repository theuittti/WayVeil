package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"regexp"
	"strings"

	"github.com/wayveil/wayveil/internal/remote"
	"github.com/wayveil/wayveil/internal/targetcheck"
	"github.com/wayveil/wayveil/rules"
)

const scannerPath = "/tmp/wv-scan"

type SuggestionKind string

const (
	KindNearby  SuggestionKind = "nearby"
	KindCountry SuggestionKind = "country"
	KindRussian SuggestionKind = "russian"
)

type Suggestion struct {
	Kind        SuggestionKind     `json:"kind"`
	Domain      string             `json:"domain"`
	Report      targetcheck.Report `json:"report"`
	Recommended bool               `json:"recommended"`
}

var countryRe = regexp.MustCompile(`^[A-Z]{2}$`)

// ensureScanner uploads wv-scan for arch unless an identical copy is there.
func ensureScanner(ctx context.Context, r remote.Runner, arch string) error {
	bin, err := files.ReadFile("assets/wv-scan-linux-" + arch)
	if err != nil {
		return errors.New("scanner for " + arch + " not built (run make scanner)")
	}
	sum := sha256.Sum256(bin)
	if out, err := r.Run(ctx, "sha256sum "+scannerPath+" 2>/dev/null || true", nil); err == nil &&
		strings.HasPrefix(string(out), hex.EncodeToString(sum[:])) {
		return nil
	}
	_, err = r.Run(ctx, "install -m 0755 /dev/stdin "+scannerPath, bin)
	return err
}

// CleanupScanner removes the uploaded scanner from the server.
func CleanupScanner(ctx context.Context, r remote.Runner) error {
	_, err := r.Run(ctx, "rm -f "+scannerPath, nil)
	return err
}

// CheckTarget checks a domain from the server itself.
func CheckTarget(ctx context.Context, r remote.Runner, arch, domain string) (targetcheck.Report, error) {
	var rep targetcheck.Report
	if err := ensureScanner(ctx, r, arch); err != nil {
		return rep, err
	}
	out, err := r.Run(ctx, scannerPath+" check -domain "+remote.ShellQuote(domain)+" -timeout 8s", nil)
	if err != nil {
		return rep, err
	}
	err = json.Unmarshal(out, &rep)
	return rep, err
}

// SuggestTargets streams target suggestions: verified presets for the
// server's country and Russian presets first, then neighbours found by the
// scan (recommended). With no neighbour, the fastest country preset is
// re-emitted as recommended. The scanner is removed at the end.
func SuggestTargets(ctx context.Context, r remote.Runner, h HostInfo, t rules.Targets, on func(Suggestion)) (err error) {
	if err := ensureScanner(ctx, r, h.Arch); err != nil {
		return err
	}
	defer func() {
		if cerr := CleanupScanner(context.WithoutCancel(ctx), r); err == nil {
			err = cerr
		}
	}()

	country := ""
	if out, err := r.Run(ctx, "curl -fsS --max-time 5 https://ipinfo.io/country 2>/dev/null || true", nil); err == nil {
		if c := strings.ToUpper(strings.TrimSpace(string(out))); countryRe.MatchString(c) {
			country = c
		}
	}
	kind := map[string]SuggestionKind{}
	var presets []string
	for _, d := range t.ByCountry[country] {
		kind[d] = KindCountry
		presets = append(presets, d)
	}
	for _, d := range t.Russian {
		if _, dup := kind[d]; !dup {
			kind[d] = KindRussian
			presets = append(presets, d)
		}
	}

	var best *Suggestion
	if len(presets) > 0 {
		cmd := "for d in"
		for _, d := range presets {
			cmd += " " + remote.ShellQuote(d)
		}
		cmd += "; do " + scannerPath + ` check -domain "$d" -timeout 5s & done; wait`
		err := r.Stream(ctx, cmd, nil, func(line string) {
			var rep targetcheck.Report
			if json.Unmarshal([]byte(line), &rep) != nil || !rep.OK {
				return
			}
			k, known := kind[rep.Domain]
			if !known {
				return
			}
			s := Suggestion{Kind: k, Domain: rep.Domain, Report: rep}
			on(s)
			if k == KindCountry && (best == nil || rep.HandshakeMs < best.Report.HandshakeMs) {
				b := s
				best = &b
			}
		})
		if err != nil {
			return err
		}
	}

	nearby := 0
	if ip, perr := netip.ParseAddr(h.PublicIP); perr == nil && ip.Is4() {
		err := r.Stream(ctx, scannerPath+" scan -self "+remote.ShellQuote(ip.String()), nil, func(line string) {
			var ev struct {
				Type   string             `json:"type"`
				Domain string             `json:"domain"`
				Report targetcheck.Report `json:"report"`
			}
			if json.Unmarshal([]byte(line), &ev) != nil || ev.Type != "found" {
				return
			}
			nearby++
			on(Suggestion{Kind: KindNearby, Domain: ev.Domain, Report: ev.Report, Recommended: true})
		})
		if err != nil {
			return err
		}
	}
	if nearby == 0 && best != nil {
		best.Recommended = true
		on(*best)
	}
	return nil
}
