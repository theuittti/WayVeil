// Package testenv starts a throwaway systemd + sshd Ubuntu container that
// integration tests use as a fake VPS. Tests skip unless WAYVEIL_IT=1.
package testenv

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/theuittti/WayVeil/internal/sshx"
)

const image = "wayveil-it"

type Env struct {
	Host      string
	Port      int
	Container string
}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func docker(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Start builds (cached) and runs a fresh container; it is removed on cleanup.
func Start(t *testing.T) *Env {
	t.Helper()
	if os.Getenv("WAYVEIL_IT") != "1" {
		t.Skip("integration test: set WAYVEIL_IT=1")
	}
	docker(t, "build", "-q", "-t", image, filepath.Join(repoRoot(), "test", "server"))
	// Not --privileged: /proc/sys stays read-only, so server scripts cannot
	// change the developer machine's global sysctls (BBR step then warns,
	// which is exactly the OpenVZ/LXC case we must handle).
	id := docker(t, "run", "-d", "--cgroupns=host", "--tmpfs", "/run", "--tmpfs", "/run/lock",
		"--tmpfs", "/tmp:exec", "-v", "/sys/fs/cgroup:/sys/fs/cgroup:rw", "-p", "127.0.0.1::22", image)
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", id).Run() })
	hp := docker(t, "port", id, "22/tcp")
	hp = strings.Split(hp, "\n")[0]
	host, portStr, err := net.SplitHostPort(hp)
	if err != nil {
		t.Fatalf("docker port %q: %v", hp, err)
	}
	port, _ := strconv.Atoi(portStr)
	e := &Env{Host: host, Port: port, Container: id}
	deadline := time.Now().Add(90 * time.Second)
	for {
		c, err := sshx.Connect(context.Background(), e.Target("root", "wayveil"))
		if err == nil {
			c.Close()
			return e
		}
		if time.Now().After(deadline) {
			t.Fatalf("ssh never came up: %v", err)
		}
		time.Sleep(time.Second)
	}
}

func (e *Env) Target(user, password string) sshx.Target {
	return sshx.Target{Host: e.Host, Port: e.Port, User: user, Password: password}
}

// Root connects as root with the password.
func (e *Env) Root(t *testing.T) *sshx.Client {
	t.Helper()
	return e.Connect(t, "root", "wayveil")
}

func (e *Env) Connect(t *testing.T, user, password string) *sshx.Client {
	t.Helper()
	c, err := sshx.Connect(context.Background(), e.Target(user, password))
	if err != nil {
		t.Fatalf("connect %s: %v", user, err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// Sh runs a command as root and returns trimmed stdout, failing the test on error.
func Sh(t *testing.T, c *sshx.Client, cmd string) string {
	t.Helper()
	out, err := c.Run(context.Background(), cmd, nil)
	if err != nil {
		t.Fatalf("%s: %v", cmd, err)
	}
	return strings.TrimSpace(string(out))
}
