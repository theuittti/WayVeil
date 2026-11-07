// Command wv-scan is uploaded temporarily to the server to check REALITY
// targets and scan the server's network for candidates. Output is JSON.
//
//	wv-scan check -domain D [-timeout 5s]
//	wv-scan scan -self IP [-limit 5 -deadline 30s -concurrency 128 -timeout 1500ms]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/theuittti/WayVeil/internal/targetcheck"
	"github.com/theuittti/WayVeil/internal/targetscan"
)

type deps struct {
	check func(context.Context, string, targetcheck.Options) targetcheck.Report
	scan  func(context.Context, targetscan.Options, func(targetscan.Candidate)) (int, error)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, deps{check: targetcheck.Check, scan: targetscan.Scan}))
}

func run(args []string, w io.Writer, d deps) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: wv-scan check|scan ...")
		return 2
	}
	enc := json.NewEncoder(w)
	ctx := context.Background()
	switch args[0] {
	case "check":
		fs := flag.NewFlagSet("check", flag.ContinueOnError)
		domain := fs.String("domain", "", "domain to check")
		timeout := fs.Duration("timeout", 5*time.Second, "overall timeout")
		if fs.Parse(args[1:]) != nil || *domain == "" {
			return 2
		}
		enc.Encode(d.check(ctx, *domain, targetcheck.Options{Timeout: *timeout}))
		return 0
	case "scan":
		fs := flag.NewFlagSet("scan", flag.ContinueOnError)
		self := fs.String("self", "", "server IPv4 address")
		limit := fs.Int("limit", 5, "stop after N candidates")
		deadline := fs.Duration("deadline", 30*time.Second, "overall deadline")
		conc := fs.Int("concurrency", 128, "parallel probes")
		timeout := fs.Duration("timeout", 1500*time.Millisecond, "per-address timeout")
		if fs.Parse(args[1:]) != nil {
			return 2
		}
		ip, err := netip.ParseAddr(*self)
		if err != nil {
			fmt.Fprintln(os.Stderr, "bad -self:", err)
			return 2
		}
		var mu sync.Mutex
		n, err := d.scan(ctx, targetscan.Options{
			Self: ip, Limit: *limit, Deadline: *deadline, Concurrency: *conc, PerAddrTimeout: *timeout,
		}, func(c targetscan.Candidate) {
			mu.Lock()
			defer mu.Unlock()
			enc.Encode(struct {
				Type string `json:"type"`
				targetscan.Candidate
			}{"found", c})
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		enc.Encode(map[string]any{"type": "done", "scanned": n})
		return 0
	}
	return 2
}
