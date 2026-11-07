// Package sshx is the SSH implementation of remote.Runner.
package sshx

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/wayveil/wayveil/internal/remote"
)

type Target struct {
	Host          string
	Port          int    // default 22
	User          string // default root
	Password      string
	PrivateKeyPEM []byte
	HostKey       string // authorized_keys format; empty = trust on first use
}

var (
	ErrHostKeyMismatch = errors.New("host_key_mismatch")
	ErrAuth            = errors.New("auth_failed")
)

type Client struct {
	user    string
	conn    *ssh.Client
	hostKey ssh.PublicKey
}

var _ remote.Runner = (*Client)(nil)

// Connect opens an SSH connection. With an empty Target.HostKey any host key
// is accepted and exposed via HostKey/Fingerprint for the caller to confirm.
func Connect(ctx context.Context, t Target) (*Client, error) {
	if t.Port == 0 {
		t.Port = 22
	}
	if t.User == "" {
		t.User = "root"
	}
	var auth []ssh.AuthMethod
	if len(t.PrivateKeyPEM) > 0 {
		signer, err := ssh.ParsePrivateKey(t.PrivateKeyPEM)
		if err != nil {
			return nil, err
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if t.Password != "" {
		pw := t.Password
		auth = append(auth, ssh.Password(pw), ssh.KeyboardInteractive(
			func(_, _ string, qs []string, _ []bool) ([]string, error) {
				ans := make([]string, len(qs))
				for i := range ans {
					ans[i] = pw
				}
				return ans, nil
			}))
	}
	var expected ssh.PublicKey
	if t.HostKey != "" {
		k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(t.HostKey))
		if err != nil {
			return nil, err
		}
		expected = k
	}
	c := &Client{user: t.User}
	cfg := &ssh.ClientConfig{
		User: t.User, Auth: auth, Timeout: 15 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			c.hostKey = key
			if expected != nil && !bytes.Equal(expected.Marshal(), key.Marshal()) {
				return ErrHostKeyMismatch
			}
			return nil
		},
	}
	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	var d net.Dialer
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	sc, chans, reqs, err := ssh.NewClientConn(raw, addr, cfg)
	if err != nil {
		raw.Close()
		if errors.Is(err, ErrHostKeyMismatch) {
			return nil, ErrHostKeyMismatch
		}
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, ErrAuth
		}
		return nil, err
	}
	c.conn = ssh.NewClient(sc, chans, reqs)
	return c, nil
}

func (c *Client) HostKey() string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(c.hostKey)))
}

func (c *Client) Fingerprint() string { return ssh.FingerprintSHA256(c.hostKey) }

func (c *Client) Close() error { return c.conn.Close() }

// wrap runs cmd through passwordless sudo for non-root users.
func wrap(user, cmd string) string {
	if user == "root" {
		return cmd
	}
	return "sudo -n bash -c " + remote.ShellQuote(cmd)
}

func (c *Client) Run(ctx context.Context, cmd string, stdin []byte) ([]byte, error) {
	var out bytes.Buffer
	err := c.exec(ctx, cmd, stdin, func(s *ssh.Session) { s.Stdout = &out })
	return out.Bytes(), err
}

func (c *Client) Stream(ctx context.Context, cmd string, stdin []byte, onLine func(string)) error {
	pr, pw := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for sc.Scan() {
			onLine(sc.Text())
		}
		pr.Close()
	}()
	err := c.exec(ctx, cmd, stdin, func(s *ssh.Session) { s.Stdout = pw })
	pw.Close()
	<-done
	return err
}

func (c *Client) exec(ctx context.Context, cmd string, stdin []byte, setup func(*ssh.Session)) error {
	s, err := c.conn.NewSession()
	if err != nil {
		return err
	}
	defer s.Close()
	var stderr bytes.Buffer
	s.Stderr = &stderr
	s.Stdin = bytes.NewReader(stdin)
	setup(s)
	errc := make(chan error, 1)
	go func() { errc <- s.Run(wrap(c.user, cmd)) }()
	select {
	case <-ctx.Done():
		_ = s.Signal(ssh.SIGKILL)
		s.Close()
		return ctx.Err()
	case err = <-errc:
	}
	if err == nil {
		return nil
	}
	if c.user != "root" && strings.Contains(stderr.String(), "sudo: a password is required") {
		return remote.ErrSudoPassword
	}
	var ee *ssh.ExitError
	if errors.As(err, &ee) {
		return &remote.ExitError{Code: ee.ExitStatus(), Stderr: stderr.String()}
	}
	return err
}
