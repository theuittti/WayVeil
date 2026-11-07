// Package ownerstore keeps the owner's server profiles (host, pinned host
// key, the app's private key). It never stores the SSH password.
// Plan 3 adds OS-level encryption (DPAPI / keyring); for now files are 0600.
package ownerstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type ServerProfile struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	User           string `json:"user"`
	HostKey        string `json:"hostKey"`
	PrivateKeyPEM  []byte `json:"privateKey"`
	AuthorizedLine string `json:"authorizedLine"`
}

// Dir is the app config directory: %APPDATA%\Wayveil or ~/.config/wayveil.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	name := "wayveil"
	if runtime.GOOS == "windows" {
		name = "Wayveil"
	}
	return filepath.Join(base, name), nil
}

func path(host string, port int) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	safe := strings.NewReplacer(":", "_", "/", "_", "\\", "_").Replace(host)
	return filepath.Join(d, "servers", fmt.Sprintf("%s_%d.json", safe, port)), nil
}

func Save(p ServerProfile) error {
	f, err := path(p.Host, p.Port)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f)
}

func Load(host string, port int) (ServerProfile, error) {
	var p ServerProfile
	f, err := path(host, port)
	if err != nil {
		return p, err
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(b, &p)
	return p, err
}

func Delete(host string, port int) error {
	f, err := path(host, port)
	if err != nil {
		return err
	}
	return os.Remove(f)
}
