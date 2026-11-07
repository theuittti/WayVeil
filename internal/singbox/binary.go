package singbox

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// Find locates the sing-box binary: $WAYVEIL_SINGBOX, next to the running
// executable, then <repo>/build/sing-box (development).
func Find() (string, error) {
	name := "sing-box"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	var cands []string
	if p := os.Getenv("WAYVEIL_SINGBOX"); p != "" {
		cands = append(cands, p)
	}
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(exe), name))
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		cands = append(cands, filepath.Join(filepath.Dir(file), "..", "..", "build", name))
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return filepath.Abs(c)
		}
	}
	return "", errors.New("sing-box binary not found (set WAYVEIL_SINGBOX or run make singbox)")
}
