package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for name, f := range map[string]func() (string, error){"config": ConfigDir, "cache": CacheDir} {
		d, err := f()
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(d) != "wayveil" {
			t.Fatalf("%s dir %s", name, d)
		}
		st, err := os.Stat(d)
		if err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
			t.Fatalf("%s: %v %v", name, st, err)
		}
	}
}
