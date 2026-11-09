package gui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/theuittti/WayVeil/internal/remote"
	"github.com/theuittti/WayVeil/internal/server"
	"github.com/theuittti/WayVeil/internal/sshx"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

const lost = "The connection to the server was lost. Connect again."

func lostDuring(step string) string {
	return "The connection to the server was lost during “" + step + "”. Connect again — Wayveil will offer to clean up and install again."
}

func TestHuman(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"auth", fmt.Errorf("connect: %w", sshx.ErrAuth), "Could not log in: check the password or key."},
		{"hostkey", sshx.ErrHostKeyMismatch, "The server's identity changed since last time. This may be an attack — not continuing."},
		{"sudo", remote.ErrSudoPassword, "This user needs a password for sudo. Use root or a user with passwordless sudo."},
		{"refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, "Cannot reach the server. Check the IP and port, and that the hosting firewall allows SSH."},
		{"timeout", &net.OpError{Op: "dial", Net: "tcp", Err: timeoutErr{}}, "Cannot reach the server. Check the IP and port, and that the hosting firewall allows SSH."},
		{"dns", &net.DNSError{Err: "no such host", Name: "nope.invalid", IsNotFound: true}, "Cannot reach the server. Check the IP and port, and that the hosting firewall allows SSH."},
		{"unsupported", &server.ScriptError{Code: "unsupported_os", Message: "centos 7"}, "Unsupported system: centos 7. Wayveil supports Ubuntu 22.04/24.04/26.04 and Debian 12/13 — reinstall the server with Ubuntu 24.04 in your hosting panel."},
		{"portbusy", &server.ScriptError{Code: "port_busy", Message: "nginx"}, "Port 443 is used by nginx. Free it and try again."},
		{"foreign", server.ErrForeignXray, "Another Xray installation is on this server. Remove it first: sudo systemctl disable --now xray && sudo rm -f /usr/local/bin/xray /etc/systemd/system/xray.service"},
		{"partial", server.ErrPartialInstall, "A previous installation did not finish."},
		{"busy", ErrBusy, "Already working on it…"},
		{"badhost", ErrBadHost, "Enter the server's IP address or hostname."},
		{"script", &StepError{Step: "Packages", Err: &server.ScriptError{Code: "install_failed", Message: "apt is locked"}}, "Installation failed at step Packages: apt is locked"},
		{"script-nostep", &server.ScriptError{Code: "bad_args", Message: "missing user"}, "Installation failed at step bad_args: missing user"},
		{"other", errors.New("boom"), "Something went wrong: boom"},
		{"wrapped-ui", uiError(sshx.ErrAuth), "Could not log in: check the password or key."},

		// Connection lost after login, with and without the running step.
		{"lost-step-eof", &StepError{Step: "Packages", Err: io.ErrUnexpectedEOF}, lostDuring("Packages")},
		{"lost-step-exitmissing", &StepError{Step: "Xray", Err: &ssh.ExitMissingError{}}, lostDuring("Xray")},
		{"lost-step-ui", uiError(&StepError{Step: "Service", Err: fmt.Errorf("stream: %w", io.EOF)}), lostDuring("Service")},
		{"lost-step-reset", &StepError{Step: "Packages", Err: &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}}, lostDuring("Packages")},
		{"lost-eof", io.EOF, lost},
		{"lost-wrapped-eof", fmt.Errorf("precheck: %w", io.ErrUnexpectedEOF), lost},
		{"lost-exitmissing", &ssh.ExitMissingError{}, lost},
		{"lost-closed", &net.OpError{Op: "write", Net: "tcp", Err: net.ErrClosed}, lost},
		{"lost-read-timeout", &net.OpError{Op: "read", Net: "tcp", Err: timeoutErr{}}, lost},
		{"lost-pipe", fmt.Errorf("x: %w", syscall.EPIPE), lost},
		{"lost-cancel", context.Canceled, lost},
		{"lost-ui", uiError(io.EOF), lost},

		// ctl.sh failures (Adopt, AddClient, Info).
		{"ctl-not-installed", &server.ScriptError{Code: "not_installed"}, "Wayveil is not installed on this server."},
		{"ctl-no-user", &server.ScriptError{Code: "no_such_user", Message: "bob"}, "This user does not exist on the server."},
		{"ctl-busy", &server.ScriptError{Code: "busy"}, "The server is busy with another Wayveil operation. Try again in a minute."},
		{"ctl-failed", &server.ScriptError{Code: "ctl_failed", Message: "line 42"}, "The server could not finish the operation: line 42"},
		{"ctl-other", ctlErr(&server.ScriptError{Code: "invalid_config", Message: "bad json"}), "The server could not finish the operation: bad json"},
		{"ctl-other-nomsg", ctlErr(&server.ScriptError{Code: "bad_key"}), "The server could not finish the operation: bad_key"},
		{"ctl-lost", ctlErr(io.EOF), lost},
		{"ctl-plain", ctlErr(errors.New("ctl add: no result")), "Something went wrong: ctl add: no result"},
	}
	for _, c := range cases {
		if got := Human(c.err); got != c.want {
			t.Errorf("%s: Human = %q, want %q", c.name, got, c.want)
		}
	}
	if Human(nil) != "" {
		t.Error("Human(nil) must be empty")
	}
}
