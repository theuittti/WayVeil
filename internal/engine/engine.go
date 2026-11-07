// Package engine runs and supervises the client's two processes: the local
// xray client (last hop to the server) and mihomo (TUN, rules, DNS) in front
// of it. It builds both configs, starts them detached, and reports status via
// mihomo's Clash API.
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
	"github.com/theuittti/WayVeil/internal/xrayclient"
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
	PID         int              `json:"pid"` // mihomo
	Config      string           `json:"config"`
	XrayPID     int              `json:"xrayPid"`
	XrayConfig  string           `json:"xrayConfig"`
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

// ownedBy checks pid is alive and really runs with our config (guards
// against PID reuse).
func ownedBy(pid int, config string) bool {
	if !alive(pid) {
		return false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return true
	}
	return bytes.Contains(b, []byte(config))
}

func ours(r runInfo) bool     { return ownedBy(r.PID, r.Config) }
func xrayOurs(r runInfo) bool { return ownedBy(r.XrayPID, r.XrayConfig) }

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

// Connect builds both configs and starts xray then mihomo, replacing any
// previous run.
func (e *Engine) Connect(ctx context.Context, s clientstore.Settings) error {
	if s.Profile == nil {
		return ErrNoProfile
	}
	if err := e.Disconnect(ctx); err != nil {
		return err
	}
	mihomoBin, err := mihomo.Find()
	if err != nil {
		return err
	}
	xrayBin, err := xrayclient.Find()
	if err != nil {
		return err
	}
	if err := e.preflight(mihomoBin); err != nil {
		return err
	}
	ips, err := e.serverIPs(ctx, s.Profile.Params.Host)
	if err != nil {
		return err
	}
	dial := ips[0]
	for _, ip := range ips {
		if ip.Is4() && !dial.Is4() {
			dial = ip
		}
	}
	if _, err := lists.Ensure(lists.Options{Dir: e.listsDir}); err != nil {
		return err
	}

	// ---- xray: the last hop ----
	socksAddr, err := freeAddr()
	if err != nil {
		return err
	}
	_, portStr, _ := net.SplitHostPort(socksAddr)
	socksPort, _ := strconv.Atoi(portStr)
	user, pass := "wayveil", randHex(16)
	xcfg, err := xrayclient.Build(xrayclient.Input{Server: s.Profile.Params, ServerIP: dial.String(),
		SocksPort: socksPort, User: user, Pass: pass, LogLevel: xrayLogLevel(e.o.LogLevel)})
	if err != nil {
		return err
	}
	xrayCfg := e.file("xray.json")
	if err := os.WriteFile(xrayCfg, xcfg, 0o600); err != nil {
		return err
	}
	if err := check(ctx, xrayBin, "xray", "run", "-test", "-c", xrayCfg); err != nil {
		return err
	}

	// ---- mihomo: TUN, rules, DNS ----
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
		XrayPort: socksPort, XrayUser: user, XrayPass: pass,
	})
	if err != nil {
		return err
	}
	cfgPath := e.file("config.yaml")
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil {
		return err
	}
	if err := check(ctx, mihomoBin, "mihomo", "-t", "-d", e.home, "-f", cfgPath); err != nil {
		return err
	}

	xrayPID, err := e.start(xrayBin, "xray.log", "run", "-c", xrayCfg)
	if err != nil {
		return err
	}
	r := runInfo{XrayPID: xrayPID, XrayConfig: xrayCfg, Config: cfgPath, ClashAddr: clashAddr, ClashSecret: secret,
		Server: s.Profile.Name, Mode: s.Mode, Since: time.Now()}
	if err := e.writeRun(r); err != nil {
		syscall.Kill(xrayPID, syscall.SIGKILL)
		return err
	}
	if err := waitTCP(socksAddr, xrayPID, 5*time.Second); err != nil {
		msg := lastLines(e.read("xray.log"), 5)
		e.Disconnect(ctx)
		return fmt.Errorf("xray did not start: %v: %s", err, msg)
	}
	r.PID, err = e.start(mihomoBin, "mihomo.log", "-d", e.home, "-f", cfgPath)
	if err != nil {
		e.Disconnect(ctx)
		return err
	}
	if err := e.writeRun(r); err != nil {
		e.Disconnect(ctx)
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

func xrayLogLevel(l string) string {
	if l == "debug" || l == "info" {
		return "info"
	}
	return "warning"
}

// check validates a config with the binary's own test mode (20s timeout:
// a wrong invocation must never hang Connect).
func check(ctx context.Context, bin, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s rejected the config: %s", name, lastLines(string(out), 3))
	}
	return nil
}

// start launches bin detached (own session: survives the CLI exiting).
func (e *Engine) start(bin, logName string, args ...string) (int, error) {
	logf, err := os.OpenFile(e.file(logName), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer logf.Close()
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	go cmd.Wait() // reap if it dies while we are still running
	return cmd.Process.Pid, nil
}

func (e *Engine) writeRun(r runInfo) error {
	b, _ := json.Marshal(r)
	return os.WriteFile(e.file("run.json"), b, 0o600)
}

func (e *Engine) read(name string) string {
	b, _ := os.ReadFile(e.file(name))
	return string(b)
}

func waitTCP(addr string, pid int, d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return errors.New("process exited")
		}
		if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			c.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("socks port not listening")
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

// Disconnect stops mihomo (its TUN routes go first), then xray. Idempotent.
func (e *Engine) Disconnect(ctx context.Context) error {
	if r, ok := e.readRun(); ok {
		if ours(r) {
			stop(r.PID)
		}
		if xrayOurs(r) {
			stop(r.XrayPID)
		}
	}
	err := os.Remove(e.file("run.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// stop sends SIGTERM, then SIGKILL after 5s.
func stop(pid int) {
	syscall.Kill(pid, syscall.SIGTERM)
	deadline := time.Now().Add(5 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if alive(pid) {
		syscall.Kill(pid, syscall.SIGKILL)
		for i := 0; i < 40 && alive(pid); i++ {
			time.Sleep(50 * time.Millisecond)
		}
	}
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
	if !xrayOurs(r) {
		st.State, st.Error = Failed, "xray stopped: "+lastLines(e.read("xray.log"), 5)
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
