// Package gui is the UI-free backend of the desktop app: the exported App
// methods are what the frontend calls, progress goes out through an Emitter.
// It must not import Wails, so it is tested with plain `go test`.
package gui

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/theuittti/WayVeil/internal/client"
	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/engine"
	"github.com/theuittti/WayVeil/internal/lists"
	"github.com/theuittti/WayVeil/internal/mihomo"
	"github.com/theuittti/WayVeil/internal/ownerstore"
	"github.com/theuittti/WayVeil/internal/remote"
	"github.com/theuittti/WayVeil/internal/server"
	"github.com/theuittti/WayVeil/internal/sshx"
	"github.com/theuittti/WayVeil/internal/targetcheck"
	"github.com/theuittti/WayVeil/rules"
)

// Emitter sends an event to the frontend. It is called with App's lock held
// for scan events, so it must not call back into App.
type Emitter interface{ Emit(event string, data any) }

// Conn is an open SSH session to the owner's server (*sshx.Client).
type Conn interface {
	remote.Runner
	Fingerprint() string
	HostKey() string
	Close() error
}

// Deps are the external calls App makes, injectable for tests.
type Deps struct {
	Connect        func(ctx context.Context, t sshx.Target) (Conn, error)
	Precheck       func(ctx context.Context, r remote.Runner) (server.HostInfo, error)
	SuggestTargets func(ctx context.Context, r remote.Runner, h server.HostInfo, t rules.Targets, on func(server.Suggestion)) error
	CheckTarget    func(ctx context.Context, r remote.Runner, arch, domain string) (targetcheck.Report, error)
	CleanupScanner func(ctx context.Context, r remote.Runner) error
	Install        func(ctx context.Context, r remote.Runner, o server.InstallOptions, onStep func(server.StepEvent)) (server.InstallResult, error)
	Uninstall      func(ctx context.Context, r remote.Runner, authKeyLine string, onStep func(server.StepEvent)) error
	Adopt          func(ctx context.Context, r remote.Runner, user, keyLine string) error
	AddClient      func(ctx context.Context, r remote.Runner, name string) (server.Client, error)
	Info           func(ctx context.Context, r remote.Runner) (server.ServerInfo, error)
	GenerateKey    func() (privatePEM []byte, authorizedLine string, err error)
	LoadOwner      func(host string, port int) (ownerstore.ServerProfile, error)
	SaveOwner      func(p ownerstore.ServerProfile) error
	LoadSettings   func() (clientstore.Settings, error)
	SaveSettings   func(s clientstore.Settings) error
	Targets        func() rules.Targets
	XrayZip        func(arch string) []byte // nil: the server downloads xray itself
	Now            func() time.Time

	// Client is the connect/apply collaborators; Client.Engine is nil when
	// the engine could not be created, in which case ClientErr says why.
	Client    client.Deps
	ClientErr error
	HasOwner  func() bool // an owner server profile is saved
}

// DefaultDeps are the real implementations; Xray zips are read from exeDir.
func DefaultDeps(exeDir string) Deps {
	d := Deps{
		Connect: func(ctx context.Context, t sshx.Target) (Conn, error) {
			c, err := sshx.Connect(ctx, t)
			if err != nil {
				return nil, err // never a typed-nil *sshx.Client inside Conn
			}
			return c, nil
		},
		Precheck:       server.Precheck,
		SuggestTargets: server.SuggestTargets,
		CheckTarget:    server.CheckTarget,
		CleanupScanner: server.CleanupScanner,
		Install:        server.Install,
		Uninstall:      server.Uninstall,
		Adopt:          server.Adopt,
		AddClient:      server.AddClient,
		Info:           server.Info,
		GenerateKey:    sshx.GenerateKey,
		LoadOwner:      ownerstore.Load,
		SaveOwner:      ownerstore.Save,
		LoadSettings:   clientstore.Load,
		SaveSettings:   clientstore.Save,
		Targets:        rules.DefaultTargets,
		XrayZip:        zipReader(exeDir),
		Now:            time.Now,
		HasOwner:       ownerstore.Any,
	}
	d.Client = client.Deps{Grant: engine.GrantCaps, Update: lists.Update, Mihomo: mihomo.Find}
	eng, err := engine.New(engine.Options{Inbound: mihomo.TUN})
	if err != nil {
		d.ClientErr = err
	} else {
		d.Client.Engine = eng
	}
	return d
}

// zipReader returns the official Xray zip for a server arch from dir, or nil
// when it is not shipped there.
func zipReader(dir string) func(arch string) []byte {
	names := map[string]string{"amd64": "Xray-linux-64.zip", "arm64": "Xray-linux-arm64-v8a.zip"}
	return func(arch string) []byte {
		n, ok := names[arch]
		if !ok {
			return nil
		}
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil || len(b) == 0 {
			return nil
		}
		return b
	}
}

// App is the backend bound to the frontend.
type App struct {
	e      Emitter
	d      Deps
	ctx    context.Context // cancelled by Shutdown
	cancel context.CancelFunc

	mu         sync.Mutex
	busy       bool               // ServerConnect / Install / Adopt / Reinstall running
	workCancel context.CancelFunc // cancels the running busy call
	sess       *session

	connBusy   bool               // Connect / SetMode running
	connCancel context.CancelFunc // cancels the running connection call
	connOp     sync.Mutex         // serialises engine operations

	scanGen    uint64 // bumped on every scan stop; stale scans cannot emit
	scanCancel context.CancelFunc
	scanDone   chan struct{}
	seen       []server.Suggestion // suggestions of the current scan
}

// session is the connected owner server.
type session struct {
	conn    Conn
	host    string
	port    int
	user    string
	hostKey string // the key the session was connected with (saved with the profile)
	info    server.HostInfo
	owner   *ownerstore.ServerProfile // saved profile for host:port, if any
}

func New(e Emitter, d Deps) *App {
	ctx, cancel := context.WithCancel(context.Background())
	return &App{e: e, d: d, ctx: ctx, cancel: cancel}
}

// NewDefault is App with the real dependencies; exeDir holds the Xray zips.
func NewDefault(e Emitter, exeDir string) *App { return New(e, DefaultDeps(exeDir)) }

// Shutdown cancels running work and closes the SSH session.
func (a *App) Shutdown() {
	a.cancel()
	a.ServerDisconnect()
}

// begin takes the busy flag; the returned end releases it.
func (a *App) begin() (context.Context, func(), error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy {
		return nil, nil, ErrBusy
	}
	if err := a.ctx.Err(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.busy, a.workCancel = true, cancel
	return ctx, func() {
		cancel()
		a.mu.Lock()
		a.busy, a.workCancel = false, nil
		a.mu.Unlock()
	}, nil
}

// detach removes the session and stops the scan; the caller closes the
// connection after waitScan.
func (a *App) detachLocked() (*session, chan struct{}) {
	s := a.sess
	a.sess = nil
	return s, a.stopScanLocked()
}

// closeSession waits (bounded) for a stopped scan to clean up the scanner on
// the server, then closes the connection.
func closeSession(s *session, scanDone chan struct{}) {
	if scanDone != nil {
		select {
		case <-scanDone:
		case <-time.After(10 * time.Second):
		}
	}
	if s != nil {
		s.conn.Close()
	}
}
