// Package appdir locates (and creates) the app's config and cache directories.
package appdir

import (
	"os"
	"path/filepath"
	"runtime"
)

func name() string {
	if runtime.GOOS == "windows" {
		return "Wayveil"
	}
	return "wayveil"
}

func ensure(base string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, name())
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, os.Chmod(d, 0o700)
}

// ConfigDir is %APPDATA%\Wayveil or ~/.config/wayveil (settings, profiles).
func ConfigDir() (string, error) { return ensure(os.UserConfigDir()) }

// CacheDir is %LOCALAPPDATA%\Wayveil or ~/.cache/wayveil (lists, runtime files).
func CacheDir() (string, error) { return ensure(os.UserCacheDir()) }
