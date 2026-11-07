// Package engine runs and supervises mihomo for the client: it builds the
// config, starts the process detached, and reports status via the Clash API.
// State lives in CacheDir()/run so separate CLI invocations share it.
package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/theuittti/WayVeil/internal/appdir"
	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/lists"
	"github.com/theuittti/WayVeil/internal/mihomo"
)

type State string

const (
	Disconnected State = "disconnected"
	Connecting   State = "connecting"
	Connected    State = "connected"
	Failed       State = "failed"
)

var ErrNoProfile = errors.New("no_profile")

type Status struct {
	State    State
	Since    time.Time
	Error    string
	Up, Down int64
	Server   string
	Mode     clientstore.Mode
}

type Options struct {
	Inbound   mihomo.InboundKind
	MixedPort int
	LogLevel  string // passed to mihomo; "" = warning
	Resolve   func(ctx context.Context, host string) ([]netip.Addr, error)
}

type Engine struct {
	o        Options
	home     string // mihomo home (-d): CacheDir(); rule files must live under it
	dir      string // runtime files: home/run
	listsDir string // home/lists
}

// run.json: everything a later invocation needs to query or stop mihomo.
type runInfo struct {
	PID         int              `json:"pid"`
	Config      string           `json:"config"`
	ClashAddr   string           `json:"clashAddr"`
	ClashSecret string           `json:"clashSecret"`
	Server      string           `json:"server"`
	Mode        clientstore.Mode `json:"mode"`
	Since       time.Time        `json:"since"`
}

func New(o Options) (*Engine, error) {
	c, err := appdir.CacheDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(c, "run")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if o.Resolve == nil {
		o.Resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	ld, err := lists.DefaultDir()
	if err != nil {
		return nil, err
	}
	return &Engine{o: o, home: c, dir: dir, listsDir: ld}, nil
}

func (e *Engine) file(n string) string { return filepath.Join(e.dir, n) }

func (e *Engine) readRun() (runInfo, bool) {
	var r runInfo
	b, err := os.ReadFile(e.file("run.json"))
	if err != nil || json.Unmarshal(b, &r) != nil || r.PID <= 0 {
		return r, false
	}
	return r, true
}

func (e *Engine) pid() int { r, _ := e.readRun(); return r.PID }

func (e *Engine) clash() (Clash, error) {
	r, ok := e.readRun()
	if !ok {
		return Clash{}, errors.New("not running")
	}
	return Clash{Addr: r.ClashAddr, Secret: r.ClashSecret}, nil
}

// alive reports whether pid is a live (non-zombie) process.
func alive(pid int) bool {
	if pid <= 0 || syscall.Kill(pid, 0) != nil {
		return false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true // non-Linux: trust kill(0)
	}
	if i := bytes.LastIndexByte(b, ')'); i >= 0 && i+2 < len(b) {
		return b[i+2] != 'Z'
	}
	return true
}

// ours checks the pid really is our mihomo (guards against PID reuse).
func ours(r runInfo) bool {
	if !alive(r.PID) {
		return false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(r.PID) + "/cmdline")
	if err != nil {
		return true
	}
	return bytes.Contains(b, []byte(r.Config))
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func freeAddr() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()
	return ln.Addr().String(), nil
}

func (e *Engine) serverIPs(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip}, nil
	}
	ips, err := e.o.Resolve(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve server %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("resolve server %s: no addresses", host)
	}
	return ips, nil
}

// Connect builds the config and starts mihomo, replacing any previous run.
func (e *Engine) Connect(ctx context.Context, s clientstore.Settings) error {
	if s.Profile == nil {
		return ErrNoProfile
	}
	if err := e.Disconnect(ctx); err != nil {
		return err
	}
	bin, err := mihomo.Find()
	if err != nil {
		return err
	}
	if err := e.preflight(bin); err != nil {
		return err
	}
	ips, err := e.serverIPs(ctx, s.Profile.Params.Host)
	if err != nil {
		return err
	}
	if _, err := lists.Ensure(lists.Options{Dir: e.listsDir}); err != nil {
		return err
	}
	clashAddr, err := freeAddr()
	if err != nil {
		return err
	}
	secret := randHex(16)
	cfg, err := mihomo.Build(mihomo.Input{
		Server: s.Profile.Params, ServerIPs: ips, Mode: s.Mode,
		AlwaysProxy: s.AlwaysProxy, AlwaysDirect: s.AlwaysDirect, ListsDir: "lists",
		Inbound: e.o.Inbound, MixedPort: e.o.MixedPort,
		ClashAddr: clashAddr, ClashSecret: secret, LogLevel: e.o.LogLevel,
	})
	if err != nil {
		return err
	}
	cfgPath := e.file("config.yaml")
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil {
		return err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(checkCtx, bin, "-t", "-d", e.home, "-f", cfgPath).CombinedOutput(); err != nil {
		return fmt.Errorf("mihomo rejected the config: %s", lastLines(string(out), 3))
	}
	logf, err := os.OpenFile(e.file("mihomo.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(bin, "-d", e.home, "-f", cfgPath)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // survives the CLI exiting
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap if it dies while we are still running

	r := runInfo{PID: cmd.Process.Pid, Config: cfgPath, ClashAddr: clashAddr, ClashSecret: secret,
		Server: s.Profile.Name, Mode: s.Mode, Since: time.Now()}
	b, _ := json.Marshal(r)
	if err := os.WriteFile(e.file("run.json"), b, 0o600); err != nil {
		cmd.Process.Kill()
		return err
	}
	c := Clash{Addr: clashAddr, Secret: secret}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(r.PID) {
			msg := e.logTail(5)
			e.Disconnect(ctx)
			return fmt.Errorf("mihomo exited: %s", msg)
		}
		if c.Version(ctx) == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	e.Disconnect(ctx)
	return errors.New("mihomo did not start in 10s: " + e.logTail(5))
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

func (e *Engine) logTail(n int) string {
	b, _ := os.ReadFile(e.file("mihomo.log"))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// Disconnect stops mihomo (SIGTERM, then SIGKILL after 5s). Idempotent.
func (e *Engine) Disconnect(ctx context.Context) error {
	r, ok := e.readRun()
	if ok && ours(r) {
		syscall.Kill(r.PID, syscall.SIGTERM)
		deadline := time.Now().Add(5 * time.Second)
		for alive(r.PID) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if alive(r.PID) {
			syscall.Kill(r.PID, syscall.SIGKILL)
			for i := 0; i < 40 && alive(r.PID); i++ {
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
	err := os.Remove(e.file("run.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Status reports the current state; works from any process.
func (e *Engine) Status(ctx context.Context) Status {
	r, ok := e.readRun()
	if !ok {
		return Status{State: Disconnected}
	}
	st := Status{Since: r.Since, Server: r.Server, Mode: r.Mode}
	if !ours(r) {
		st.State, st.Error = Failed, "mihomo stopped: "+e.logTail(5)
		return st
	}
	c := Clash{Addr: r.ClashAddr, Secret: r.ClashSecret}
	if _, tr, err := c.Connections(ctx); err == nil {
		st.State, st.Up, st.Down = Connected, tr.Up, tr.Down
	} else {
		st.State = Connecting
	}
	return st
}
