// Package remote defines how server logic runs commands on a host, so the
// logic can be tested with fakes and run for real over SSH.
package remote

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Runner runs shell commands as root on the server.
type Runner interface {
	// Run executes cmd with stdin and returns stdout; a non-zero exit is *ExitError.
	Run(ctx context.Context, cmd string, stdin []byte) ([]byte, error)
	// Stream executes cmd and calls onLine for every stdout line.
	Stream(ctx context.Context, cmd string, stdin []byte, onLine func(string)) error
}

// ExitError is a non-zero exit of a remote command.
type ExitError struct {
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d: %s", e.Code, strings.TrimSpace(e.Stderr))
}

// ErrSudoPassword means the SSH user needs a password for sudo.
var ErrSudoPassword = errors.New("sudo_password_required")

// ShellQuote quotes s for POSIX shells.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// WithEnv prepends sorted `export K='v'` lines to a script.
func WithEnv(script []byte, env map[string]string) []byte {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString("export " + k + "=" + ShellQuote(env[k]) + "\n")
	}
	b.Write(script)
	return []byte(b.String())
}
