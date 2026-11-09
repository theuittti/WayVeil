package gui

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"

	"golang.org/x/crypto/ssh"

	"github.com/theuittti/WayVeil/internal/client"
	"github.com/theuittti/WayVeil/internal/clientstore"
	"github.com/theuittti/WayVeil/internal/engine"
	"github.com/theuittti/WayVeil/internal/remote"
	"github.com/theuittti/WayVeil/internal/server"
	"github.com/theuittti/WayVeil/internal/sshx"
)

var (
	// ErrBusy: another connect/install call is still running.
	ErrBusy = errors.New("busy")
	// ErrBadHost: the server address field could not be understood.
	ErrBadHost      = errors.New("bad host")
	errNotConnected = errors.New("not connected")

	// Codes printed by ctl.sh (Adopt, AddClient, Info).
	errNotInstalled = &server.ScriptError{Code: "not_installed"}
	errNoSuchUser   = &server.ScriptError{Code: "no_such_user"}
	errServerBusy   = &server.ScriptError{Code: "busy"}
	errCtlFailed    = &server.ScriptError{Code: "ctl_failed"}
)

// ctlError marks a failure of a ctl.sh call, so its ScriptError is not
// reported as an installation step.
type ctlError struct{ err error }

func (e *ctlError) Error() string { return e.err.Error() }
func (e *ctlError) Unwrap() error { return e.err }

func ctlErr(err error) error {
	if err == nil {
		return nil
	}
	return &ctlError{err}
}

// StepError is an install failure annotated with the step that was running.
type StepError struct {
	Step string // human step title
	Err  error
}

func (e *StepError) Error() string { return e.Err.Error() }
func (e *StepError) Unwrap() error { return e.Err }

// UIError is what App methods return: Error() is the Human text (what the
// frontend shows), Unwrap gives the original error.
type UIError struct {
	msg string
	err error
}

func (e *UIError) Error() string { return e.msg }
func (e *UIError) Unwrap() error { return e.err }

func uiError(err error) error {
	if err == nil {
		return nil
	}
	var u *UIError
	if errors.As(err, &u) {
		return err
	}
	return &UIError{msg: Human(err), err: err}
}

// Human turns an error into an English sentence for the screen.
func Human(err error) string {
	if err == nil {
		return ""
	}
	var u *UIError
	var se *server.ScriptError
	switch {
	case errors.As(err, &u):
		return u.msg
	case errors.Is(err, ErrBusy):
		return "Already working on it…"
	case errors.Is(err, ErrBadHost):
		return "Enter the server's IP address or hostname."
	case errors.Is(err, engine.ErrNeedsElevation):
		return "Wayveil needs permission to create its network interface."
	case errors.Is(err, engine.ErrElevationDenied):
		return "Permission was not granted."
	case errors.Is(err, client.ErrNoProfile):
		return "No connection link yet. Paste one first."
	case errors.Is(err, errNotConnected):
		return "Not connected to the server. Go back and connect again."
	case errors.Is(err, clientstore.ErrBadDomain):
		return "Enter a website domain, like www.microsoft.com."
	case errors.Is(err, sshx.ErrAuth):
		return "Could not log in: check the password or key."
	case errors.Is(err, sshx.ErrHostKeyMismatch):
		return "The server's identity changed since last time. This may be an attack — not continuing."
	case errors.Is(err, remote.ErrSudoPassword):
		return "This user needs a password for sudo. Use root or a user with passwordless sudo."
	case connectionLost(err):
		var st *StepError
		if errors.As(err, &st) && st.Step != "" {
			return "The connection to the server was lost during “" + st.Step + "”. Connect again — Wayveil will offer to clean up and install again."
		}
		return "The connection to the server was lost. Connect again."
	case unreachable(err):
		return "Cannot reach the server. Check the IP and port, and that the hosting firewall allows SSH."
	case errors.Is(err, server.ErrUnsupportedOS):
		return "Unsupported system: " + detail(err, "unknown") + ". Wayveil supports Ubuntu 22.04/24.04/26.04 and Debian 12/13 — reinstall the server with Ubuntu 24.04 in your hosting panel."
	case errors.Is(err, server.ErrPortBusy):
		return "Port 443 is used by " + detail(err, "another program") + ". Free it and try again."
	case errors.Is(err, server.ErrForeignXray):
		return "Another Xray installation is on this server. Remove it first: sudo systemctl disable --now xray && sudo rm -f /usr/local/bin/xray /etc/systemd/system/xray.service"
	case errors.Is(err, server.ErrPartialInstall):
		return "A previous installation did not finish."
	case errors.Is(err, server.ErrAlreadyInstalled):
		return "Wayveil is already installed on this server."
	case errors.Is(err, errNotInstalled):
		return "Wayveil is not installed on this server."
	case errors.Is(err, errNoSuchUser):
		return "This user does not exist on the server."
	case errors.Is(err, errServerBusy):
		return "The server is busy with another Wayveil operation. Try again in a minute."
	case errors.As(err, &se) && (errors.As(err, new(*ctlError)) || errors.Is(err, errCtlFailed)):
		msg := se.Message
		if msg == "" {
			msg = se.Code
		}
		return "The server could not finish the operation: " + msg
	case errors.As(err, &se):
		step := se.Code
		var st *StepError
		if errors.As(err, &st) && st.Step != "" {
			step = st.Step
		}
		msg := se.Message
		if msg == "" {
			msg = se.Code
		}
		return "Installation failed at step " + step + ": " + msg
	}
	return "Something went wrong: " + err.Error()
}

func detail(err error, def string) string {
	var se *server.ScriptError
	if errors.As(err, &se) && se.Message != "" {
		return se.Message
	}
	return def
}

func unreachable(err error) bool {
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return true
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return true
	}
	for _, e := range []error{syscall.ECONNREFUSED, syscall.EHOSTUNREACH, syscall.ENETUNREACH, syscall.ETIMEDOUT} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// connectionLost: the SSH session broke after login (EOF, a closed channel or
// connection, a read/write error, or the session's context cancelled).
func connectionLost(err error) bool {
	var op *net.OpError
	if errors.As(err, &op) {
		return op.Op != "dial"
	}
	var missing *ssh.ExitMissingError
	if errors.As(err, &missing) {
		return true
	}
	for _, e := range []error{io.EOF, io.ErrUnexpectedEOF, net.ErrClosed, context.Canceled,
		syscall.ECONNRESET, syscall.ECONNABORTED, syscall.EPIPE} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}
