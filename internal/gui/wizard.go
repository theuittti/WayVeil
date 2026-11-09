package gui

import (
	"context"
	"errors"
	"io/fs"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/ownerstore"
	"github.com/theuittti/WayVeil/internal/profile"
	"github.com/theuittti/WayVeil/internal/server"
	"github.com/theuittti/WayVeil/internal/sshx"
)

// AccessForm is the Access screen. Password lives only for one ServerConnect.
type AccessForm struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Password   string `json:"password"`
	PrivateKey string `json:"privateKey"`
}

type HostCheck struct {
	Fingerprint string `json:"fingerprint"` // SHA256:…
	Pinned      bool   `json:"pinned"`      // matches the saved host key (no need to confirm)
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	State       string `json:"state"`   // "fresh" | "wayveil" | "partial" | "foreign" | "unsupported" | "port-busy"
	Detail      string `json:"detail"`  // OS name, or the process on :443
	Message     string `json:"message"` // Human text for partial and refused states, else ""
}

type Done struct {
	Link string `json:"link"`
	QR   string `json:"qr"` // data:image/png;base64,…
}

// StepEvent is the payload of the "step" event.
type StepEvent struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // "start" | "ok" | "warn" | "fail"
	Title   string `json:"title"`
	Message string `json:"message"`
	At      int64  `json:"at"` // unix ms
}

var installTitles = map[string]string{
	"uninstaller": "Preparing clean removal", "authkey": "App access key", "deps": "Packages",
	"xray": "Xray", "keys": "REALITY keys", "config": "Configuration", "service": "Service",
	"bbr": "BBR", "logrotate": "Log rotation", "verify": "Final check",
}

var uninstallTitles = map[string]string{
	"service": "Stopping the old service", "files": "Removing old files", "sysctl": "Network settings",
	"packages": "Removing packages", "user": "Service user", "authkey": "Old access key",
}

const ownDevice = "Me"

// ---- host input ----

var hostnameRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*\.?$`)
var userRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

// parseHost accepts "1.2.3.4", "user@host", "ssh://user@host:port",
// "[v6]:port" or a bare IPv6 address. A port or user in the host field wins
// over the form fields; defaults are 22 and root.
func parseHost(in string, formPort int, formUser string) (host string, port int, user string, err error) {
	s := strings.TrimSpace(in)
	if len(s) >= 6 && strings.EqualFold(s[:6], "ssh://") {
		s = s[6:]
	}
	user, port = strings.TrimSpace(formUser), formPort
	if i := strings.LastIndex(s, "@"); i >= 0 {
		user, s = s[:i], s[i+1:]
		if user == "" {
			return "", 0, "", ErrBadHost
		}
	}
	host = s
	portStr := ""
	switch {
	case strings.HasPrefix(s, "["):
		end := strings.Index(s, "]")
		if end < 0 {
			return "", 0, "", ErrBadHost
		}
		host, portStr = s[1:end], s[end+1:]
		if portStr != "" {
			if !strings.HasPrefix(portStr, ":") {
				return "", 0, "", ErrBadHost
			}
			portStr = portStr[1:]
		}
		if a, perr := netip.ParseAddr(host); perr != nil || !a.Is6() {
			return "", 0, "", ErrBadHost
		}
	case strings.Count(s, ":") == 1:
		host, portStr, _ = strings.Cut(s, ":")
	}
	if portStr != "" {
		p, perr := strconv.Atoi(portStr)
		if perr != nil {
			return "", 0, "", ErrBadHost
		}
		port = p
	}
	if port == 0 {
		port = 22
	}
	if user == "" {
		user = "root"
	}
	if port < 1 || port > 65535 || !userRe.MatchString(user) {
		return "", 0, "", ErrBadHost
	}
	if _, perr := netip.ParseAddr(host); perr != nil && !hostnameRe.MatchString(host) {
		return "", 0, "", ErrBadHost
	}
	return host, port, user, nil
}

// ---- connect ----

func stateOf(h server.HostInfo) string {
	switch {
	case !h.Supported:
		return "unsupported"
	case h.PartialInstall:
		return "partial"
	case h.HasWayveil:
		return "wayveil"
	case h.HasForeignXray:
		return "foreign"
	case h.Port443Busy:
		return "port-busy"
	}
	return "fresh"
}

func hostCheck(c Conn, h server.HostInfo, pinned bool) HostCheck {
	osName := strings.TrimSpace(h.OSID + " " + h.OSVersion)
	hc := HostCheck{Fingerprint: c.Fingerprint(), Pinned: pinned, OS: osName, Arch: h.Arch,
		State: stateOf(h), Detail: osName}
	switch hc.State {
	case "unsupported":
		hc.Message = Human(&server.ScriptError{Code: server.ErrUnsupportedOS.Code, Message: osName})
	case "port-busy":
		hc.Detail = h.Port443Process
		hc.Message = Human(&server.ScriptError{Code: server.ErrPortBusy.Code, Message: h.Port443Process})
	case "foreign":
		hc.Message = Human(server.ErrForeignXray)
	case "partial":
		hc.Message = Human(server.ErrPartialInstall)
	}
	return hc
}

// ServerConnect logs in (pinning the saved host key, if any), prechecks the
// server and, when it can be installed on, starts the background target scan.
// An unpinned fingerprint must be confirmed by the user; the next
// Install/Adopt/Reinstall is that confirmation, "No" is ServerDisconnect.
func (a *App) ServerConnect(f AccessForm) (HostCheck, error) {
	host, port, user, err := parseHost(f.Host, f.Port, f.User)
	if err != nil {
		return HostCheck{}, uiError(err)
	}
	ctx, end, err := a.begin()
	if err != nil {
		return HostCheck{}, uiError(err)
	}
	defer end()
	a.mu.Lock()
	old, scanDone := a.detachLocked()
	a.mu.Unlock()
	closeSession(old, scanDone)

	t := sshx.Target{Host: host, Port: port, User: user, Password: f.Password}
	f.Password = ""
	if f.PrivateKey != "" {
		t.PrivateKeyPEM = []byte(f.PrivateKey)
	}
	var owner *ownerstore.ServerProfile
	switch p, err := a.d.LoadOwner(host, port); {
	case err == nil:
		owner = &p
		t.HostKey = p.HostKey
		if t.Password == "" && len(t.PrivateKeyPEM) == 0 {
			t.PrivateKeyPEM = p.PrivateKeyPEM
			if p.User != "" {
				t.User = p.User
			}
		}
	case !errors.Is(err, fs.ErrNotExist):
		return HostCheck{}, uiError(err)
	}
	conn, err := a.d.Connect(ctx, t)
	t.Password = ""
	if err != nil {
		return HostCheck{}, uiError(err)
	}
	info, err := a.d.Precheck(ctx, conn)
	if err != nil {
		conn.Close()
		return HostCheck{}, uiError(err)
	}
	s := &session{conn: conn, host: host, port: port, user: t.User, hostKey: conn.HostKey(), info: info, owner: owner}
	hc := hostCheck(conn, info, owner != nil && owner.HostKey != "")

	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil { // ServerDisconnect/Shutdown meanwhile
		conn.Close()
		return HostCheck{}, uiError(err)
	}
	a.sess = s
	switch hc.State {
	case "fresh", "wayveil", "partial":
		a.startScanLocked(s)
	}
	return hc, nil
}

// ServerDisconnect cancels the scan and any running call, and closes SSH.
func (a *App) ServerDisconnect() {
	a.mu.Lock()
	if a.workCancel != nil {
		a.workCancel()
	}
	s, scanDone := a.detachLocked()
	a.mu.Unlock()
	closeSession(s, scanDone)
}

// ---- install / adopt / reinstall ----

// run is the busy-guarded frame of Install/Adopt/Reinstall: it stops the scan
// so no late target events reach the screen.
func (a *App) run(fn func(ctx context.Context, s *session) (Done, error)) (Done, error) {
	ctx, end, err := a.begin()
	if err != nil {
		return Done{}, uiError(err)
	}
	defer end()
	a.mu.Lock()
	s := a.sess
	if s != nil {
		a.stopScanLocked() // its goroutine still removes the scanner in the background
	}
	a.mu.Unlock()
	if s == nil {
		return Done{}, uiError(errNotConnected)
	}
	d, err := fn(ctx, s)
	return d, uiError(err)
}

// Install sets Wayveil up on a fresh server with target sni.
func (a *App) Install(sni string) (Done, error) {
	sni, err := clientstore.NormalizeDomain(sni)
	if err != nil {
		return Done{}, uiError(err)
	}
	return a.run(func(ctx context.Context, s *session) (Done, error) {
		return a.install(ctx, s, sni)
	})
}

// Adopt takes over a server that already runs Wayveil: the app key is
// authorized and a "Me" device created, nothing is reinstalled.
func (a *App) Adopt() (Done, error) {
	return a.run(func(ctx context.Context, s *session) (Done, error) {
		if !s.info.HasWayveil {
			return Done{}, errors.New("Wayveil is not installed on this server")
		}
		line, err := a.newKey(s)
		if err != nil {
			return Done{}, err
		}
		if err := a.d.Adopt(ctx, s.conn, s.user, line); err != nil {
			return Done{}, err
		}
		return a.finish(ctx, s)
	})
}

// Reinstall removes Wayveil (also a half-finished install) and installs again.
// Old links stop working.
func (a *App) Reinstall(sni string) (Done, error) {
	sni, err := clientstore.NormalizeDomain(sni)
	if err != nil {
		return Done{}, uiError(err)
	}
	return a.run(func(ctx context.Context, s *session) (Done, error) {
		// Fresh precheck: the session's may be from before a failed install.
		info, err := a.d.Precheck(ctx, s.conn)
		if err != nil {
			return Done{}, err
		}
		a.mu.Lock() // CheckDomain reads s.info concurrently
		s.info = info
		a.mu.Unlock()
		switch stateOf(info) {
		case "unsupported":
			return Done{}, &server.ScriptError{Code: server.ErrUnsupportedOS.Code, Message: strings.TrimSpace(info.OSID + " " + info.OSVersion)}
		case "foreign":
			return Done{}, server.ErrForeignXray
		}
		authLine := ""
		if s.owner != nil {
			authLine = s.owner.AuthorizedLine
		}
		var last string
		if err := a.d.Uninstall(ctx, s.conn, authLine, a.onStep("uninstall-", uninstallTitles, &last)); err != nil {
			return Done{}, stepErr(last, err)
		}
		return a.install(ctx, s, sni)
	})
}

func (a *App) install(ctx context.Context, s *session, sni string) (Done, error) {
	line, err := a.newKey(s)
	if err != nil {
		return Done{}, err
	}
	var last string
	_, err = a.d.Install(ctx, s.conn, server.InstallOptions{Address: s.host, SNI: sni, AuthUser: s.user,
		AuthKeyLine: line, XrayZip: a.d.XrayZip(s.info.Arch)}, a.onStep("", installTitles, &last))
	if err != nil {
		return Done{}, stepErr(last, err)
	}
	return a.finish(ctx, s)
}

// newKey generates the app key and saves the owner profile (with the host key
// this session was connected with) before the server is touched, so even a
// failed install can be cleaned up.
func (a *App) newKey(s *session) (string, error) {
	pem, line, err := a.d.GenerateKey()
	if err != nil {
		return "", err
	}
	p := ownerstore.ServerProfile{Host: s.host, Port: s.port, User: s.user, HostKey: s.hostKey,
		PrivateKeyPEM: pem, AuthorizedLine: line}
	if err := a.d.SaveOwner(p); err != nil {
		return "", err
	}
	s.owner = &p
	return line, nil
}

// finish creates the "Me" device and saves its link as the client profile.
func (a *App) finish(ctx context.Context, s *session) (Done, error) {
	cl, err := a.d.AddClient(ctx, s.conn, ownDevice)
	if err != nil {
		return Done{}, err
	}
	info, err := a.d.Info(ctx, s.conn)
	if err != nil {
		return Done{}, err
	}
	link := server.ClientLink(info, cl)
	p, err := profile.FromLink(link)
	if err != nil {
		return Done{}, err
	}
	st, err := a.d.LoadSettings()
	if err != nil {
		return Done{}, err
	}
	st.Profile = &p
	if err := a.d.SaveSettings(st); err != nil {
		return Done{}, err
	}
	qr, err := qrDataURL(link)
	if err != nil {
		return Done{}, err
	}
	return Done{Link: link, QR: qr}, nil
}

// onStep emits "step" events; *last tracks the title of the last started step.
func (a *App) onStep(prefix string, titles map[string]string, last *string) func(server.StepEvent) {
	return func(ev server.StepEvent) {
		title := titles[ev.ID]
		if title == "" {
			title = ev.ID
		}
		if ev.Status == server.StepStart {
			*last = title
		}
		a.e.Emit("step", StepEvent{ID: prefix + ev.ID, Status: string(ev.Status), Title: title,
			Message: ev.Message, At: a.d.Now().UnixMilli()})
	}
}

func stepErr(step string, err error) error {
	if step == "" {
		return err
	}
	return &StepError{Step: step, Err: err}
}
