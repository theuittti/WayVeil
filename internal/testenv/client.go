package testenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const clientImage = "wayveil-it-client"

// Client is a throwaway desktop-like container running the wayveil CLI as
// the unprivileged user "app" with a capability-enabled mihomo (models the
// one-time grant). Never --privileged.
type Client struct{ ID string }

// IP returns the container's address on the default bridge.
func (e *Env) IP(t *testing.T) string {
	t.Helper()
	return docker(t, "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", e.Container)
}

// StartClient needs `make build` (build/wayveil, build/mihomo, build/xray).
func StartClient(t *testing.T) *Client {
	t.Helper()
	build := filepath.Join(repoRoot(), "build")
	docker(t, "build", "-q", "-t", clientImage, filepath.Join(repoRoot(), "test", "client"))
	id := docker(t, "run", "-d", "--init", "--cap-add", "NET_ADMIN", "--device", "/dev/net/tun", "--dns", "8.8.8.8",
		"-v", build+":/opt/wayveil:ro", clientImage)
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", id).Run() })
	c := &Client{ID: id}
	c.Root(t, "cp /opt/wayveil/mihomo /opt/wayveil/xray /opt/wayveil/wayveil /home/app/ && chown app:app /home/app/mihomo /home/app/xray /home/app/wayveil && "+
		"setcap cap_net_admin,cap_net_bind_service,cap_net_raw+ep /home/app/mihomo")
	return c
}

func (c *Client) exec(t *testing.T, user, cmd string, mustOK bool) string {
	t.Helper()
	home := "/home/" + user
	if user == "root" {
		home = "/root"
	}
	args := []string{"exec", "-u", user, "-w", home, "-e", "HOME=" + home, "-e", "WAYVEIL_MIHOMO=/home/app/mihomo", "-e", "WAYVEIL_XRAY=/home/app/xray", "-e", "WAYVEIL_LOG_LEVEL=info", c.ID, "bash", "-c", cmd}
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil && mustOK {
		t.Fatalf("client %s: %v\n%s", cmd, err, out)
	}
	return strings.TrimSpace(string(out))
}

// App runs cmd as the unprivileged user and fails the test on error.
func (c *Client) App(t *testing.T, cmd string) string { t.Helper(); return c.exec(t, "app", cmd, true) }

// AppTry runs cmd as the user and returns output even on failure.
func (c *Client) AppTry(t *testing.T, cmd string) string {
	t.Helper()
	return c.exec(t, "app", cmd, false)
}

// Root runs cmd as root inside the client container.
func (c *Client) Root(t *testing.T, cmd string) string {
	t.Helper()
	return c.exec(t, "root", cmd, true)
}

// Network creates a throwaway IPv6-enabled docker network.
func Network(t *testing.T, subnet6 string) string {
	t.Helper()
	name := "wv6-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	exec.Command("docker", "network", "rm", name).Run()
	docker(t, "network", "create", "--ipv6", "--subnet", subnet6, name)
	t.Cleanup(func() { exec.Command("docker", "network", "rm", name).Run() })
	return name
}

// Attach connects a container to network and returns its IPv6 address there.
func Attach(t *testing.T, network, container string) string {
	t.Helper()
	docker(t, "network", "connect", network, container)
	t.Cleanup(func() { exec.Command("docker", "network", "disconnect", "-f", network, container).Run() })
	return docker(t, "inspect", "-f", "{{(index .NetworkSettings.Networks \""+network+"\").GlobalIPv6Address}}", container)
}

// XrayZip returns build/Xray-linux-64.zip (kept by `make xray`) so installs
// in tests don't depend on GitHub; nil when missing (the server downloads it).
func XrayZip() []byte {
	b, _ := os.ReadFile(filepath.Join(repoRoot(), "build", "Xray-linux-64.zip"))
	return b
}
