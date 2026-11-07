// Package targetscan looks for REALITY target candidates hosted next to the
// server: it reads certificates from neighbouring IPs and verifies the names.
package targetscan

import (
	"context"
	"crypto/tls"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wayveil/wayveil/internal/targetcheck"
)

type Candidate struct {
	Domain string             `json:"domain"`
	IP     string             `json:"ip"`
	Report targetcheck.Report `json:"report"`
}

type Options struct {
	Self           netip.Addr
	Port           int           // 443
	PerAddrTimeout time.Duration // 1500ms
	Concurrency    int           // 128
	Limit          int           // 5
	Deadline       time.Duration // 30s
	Check          targetcheck.Options
	Resolve        func(ctx context.Context, host string) ([]netip.Addr, error)

	dial func(ctx context.Context, network, addr string) (net.Conn, error) // test hook
}

func (o Options) withDefaults() Options {
	if o.Port == 0 {
		o.Port = 443
	}
	if o.PerAddrTimeout == 0 {
		o.PerAddrTimeout = 1500 * time.Millisecond
	}
	if o.Concurrency == 0 {
		o.Concurrency = 128
	}
	if o.Limit == 0 {
		o.Limit = 5
	}
	if o.Deadline == 0 {
		o.Deadline = 30 * time.Second
	}
	if o.Resolve == nil {
		o.Resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
		}
	}
	if o.dial == nil {
		o.dial = (&net.Dialer{}).DialContext
	}
	return o
}

// ScanOrder lists the IPv4 addresses to probe: the server's own /24 sorted by
// distance from self, then the rest of the enclosing /22 by distance.
func ScanOrder(self netip.Addr) []netip.Addr {
	if !self.Is4() {
		return nil
	}
	s := u32(self)
	own := netip.PrefixFrom(self, 24).Masked()
	base := s &^ 0x3ff
	var in, out []netip.Addr
	for v := base; v < base+1024; v++ {
		if v == s {
			continue
		}
		a := fromU32(v)
		if own.Contains(a) {
			in = append(in, a)
		} else {
			out = append(out, a)
		}
	}
	byDist := func(list []netip.Addr) {
		sort.SliceStable(list, func(i, j int) bool { return dist(u32(list[i]), s) < dist(u32(list[j]), s) })
	}
	byDist(in)
	byDist(out)
	return append(in, out...)
}

func u32(a netip.Addr) uint32 { b := a.As4(); return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]) }
func fromU32(v uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}
func dist(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// Scan probes ScanOrder(o.Self) and calls found for every verified candidate,
// stopping after o.Limit candidates or o.Deadline. It returns how many
// addresses were probed.
func Scan(ctx context.Context, o Options, found func(Candidate)) (int, error) {
	o = o.withDefaults()
	ctx, cancel := context.WithTimeout(ctx, o.Deadline)
	defer cancel()

	ips := make(chan netip.Addr)
	var (
		mu      sync.Mutex
		seen    = map[string]bool{}
		nFound  int
		scanned int
		wg      sync.WaitGroup
	)
	claim := func(name string) bool {
		mu.Lock()
		defer mu.Unlock()
		if seen[name] || nFound >= o.Limit {
			return false
		}
		seen[name] = true
		return true
	}
	emit := func(c Candidate) {
		mu.Lock()
		defer mu.Unlock()
		if nFound >= o.Limit {
			return
		}
		nFound++
		found(c)
		if nFound >= o.Limit {
			cancel()
		}
	}
	for i := 0; i < o.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range ips {
				mu.Lock()
				scanned++
				mu.Unlock()
				for _, name := range certNames(ctx, o, ip) {
					if ctx.Err() != nil || !claim(name) {
						continue
					}
					if !resolvesPublic(ctx, o, name) {
						continue
					}
					rep := targetcheck.Check(ctx, name, o.Check)
					if rep.OK && ctx.Err() == nil {
						emit(Candidate{Domain: name, IP: ip.String(), Report: rep})
					}
				}
			}
		}()
	}
feed:
	for _, ip := range ScanOrder(o.Self) {
		select {
		case ips <- ip:
		case <-ctx.Done():
			break feed
		}
	}
	close(ips)
	wg.Wait()
	return scanned, nil
}

// certNames handshakes with ip and returns the usable DNS names of its leaf
// certificate (CN + SANs, without wildcards and IP literals).
func certNames(ctx context.Context, o Options, ip netip.Addr) []string {
	ctx, cancel := context.WithTimeout(ctx, o.PerAddrTimeout)
	defer cancel()
	raw, err := o.dial(ctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(o.Port)))
	if err != nil {
		return nil
	}
	defer raw.Close()
	conn := tls.Client(raw, &tls.Config{InsecureSkipVerify: true, ServerName: ip.String(), NextProtos: []string{"h2", "http/1.1"}})
	if err := conn.HandshakeContext(ctx); err != nil {
		return nil
	}
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil
	}
	leaf := certs[0]
	var out []string
	add := func(n string) {
		n = strings.ToLower(strings.TrimSuffix(n, "."))
		if n == "" || strings.Contains(n, "*") || !strings.Contains(n, ".") {
			return
		}
		if _, err := netip.ParseAddr(n); err == nil {
			return
		}
		for _, x := range out {
			if x == n {
				return
			}
		}
		out = append(out, n)
	}
	for _, n := range leaf.DNSNames {
		add(n)
	}
	add(leaf.Subject.CommonName)
	return out
}

func resolvesPublic(ctx context.Context, o Options, name string) bool {
	addrs, err := o.Resolve(ctx, name)
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if !a.IsPrivate() && !a.IsLoopback() && !a.IsLinkLocalUnicast() && !a.IsUnspecified() {
			return true
		}
	}
	return false
}
