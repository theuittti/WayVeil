package gui

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/server"
	"github.com/theuittti/WayVeil/internal/targetcheck"
)

// TargetEvent is the payload of the "target" event.
type TargetEvent struct {
	Domain      string `json:"domain"`
	Kind        string `json:"kind"` // "nearby" | "country" | "russian"
	Recommended bool   `json:"recommended"`
	HandshakeMs int    `json:"handshakeMs"`
}

type DomainCheck struct {
	OK           bool     `json:"ok"`
	Problems     []string `json:"problems"`
	Alternatives []string `json:"alternatives"`
}

func (a *App) startScanLocked(s *session) {
	a.stopScanLocked()
	gen := a.scanGen
	ctx, cancel := context.WithCancel(a.ctx)
	done := make(chan struct{})
	a.scanCancel, a.scanDone, a.seen = cancel, done, nil
	targets, info := a.d.Targets(), s.info
	go func() {
		defer close(done)
		_ = a.d.SuggestTargets(ctx, s.conn, info, targets, func(sg server.Suggestion) {
			a.mu.Lock()
			defer a.mu.Unlock()
			if gen != a.scanGen {
				return
			}
			a.seen = append(a.seen, sg)
			a.e.Emit("target", TargetEvent{Domain: sg.Domain, Kind: string(sg.Kind),
				Recommended: sg.Recommended, HandshakeMs: sg.Report.HandshakeMs})
		})
		a.mu.Lock()
		defer a.mu.Unlock()
		if gen == a.scanGen {
			cancel()
			a.scanCancel, a.scanDone = nil, nil
			a.e.Emit("target-done", struct{}{})
		}
	}()
}

// stopScanLocked cancels the scan; from now on it cannot emit. The returned
// channel closes when its goroutine (and the scanner cleanup) has finished.
func (a *App) stopScanLocked() chan struct{} {
	a.scanGen++
	if a.scanCancel != nil {
		a.scanCancel()
	}
	done := a.scanDone
	a.scanCancel, a.scanDone = nil, nil
	return done
}

// alternativesLocked: up to 3 seen domains other than skip, recommended first.
func (a *App) alternativesLocked(skip string) []string {
	out := []string{}
	have := map[string]bool{skip: true}
	for _, rec := range []bool{true, false} {
		for _, s := range a.seen {
			if len(out) == 3 {
				return out
			}
			if (rec && !s.Recommended) || have[s.Domain] {
				continue
			}
			have[s.Domain] = true
			out = append(out, s.Domain)
		}
	}
	return out
}

func problemText(rep targetcheck.Report, p targetcheck.Problem) string {
	switch p {
	case targetcheck.Unreachable:
		return "The site can't be reached from your server."
	case targetcheck.NoTLS13:
		return "The site doesn't support TLS 1.3."
	case targetcheck.NoHTTP2:
		return "The site doesn't support HTTP/2."
	case targetcheck.RedirectOther:
		if rep.RedirectHost != "" {
			return "The site redirects to another domain (" + rep.RedirectHost + ")."
		}
		return "The site redirects to another domain."
	case targetcheck.BadCert:
		return "The site's certificate isn't valid."
	case targetcheck.Slow:
		return "The site answers slowly from your server (" + strconv.Itoa(rep.HandshakeMs) + " ms)."
	}
	return "The site failed a check (" + strings.ReplaceAll(string(p), "_", " ") + ")."
}

// CheckDomain checks the user's own target domain from the server.
func (a *App) CheckDomain(domain string) (DomainCheck, error) {
	d, err := clientstore.NormalizeDomain(domain)
	if err != nil {
		return DomainCheck{}, uiError(err)
	}
	a.mu.Lock()
	s := a.sess
	arch := ""
	if s != nil {
		arch = s.info.Arch
	}
	a.mu.Unlock()
	if s == nil {
		return DomainCheck{}, uiError(errNotConnected)
	}
	ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
	defer cancel()
	rep, err := a.d.CheckTarget(ctx, s.conn, arch, d)
	a.mu.Lock()
	idle := a.scanCancel == nil && a.sess == s // no scan will remove the scanner for us
	alts := a.alternativesLocked(d)
	a.mu.Unlock()
	if idle {
		_ = a.d.CleanupScanner(ctx, s.conn)
	}
	if err != nil {
		return DomainCheck{}, uiError(err)
	}
	dc := DomainCheck{OK: rep.OK, Problems: []string{}, Alternatives: []string{}}
	for _, p := range rep.Problems {
		dc.Problems = append(dc.Problems, problemText(rep, p))
	}
	if !rep.OK {
		dc.Alternatives = alts
	}
	return dc, nil
}
