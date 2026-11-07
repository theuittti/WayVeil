package server

import (
	"context"
	"strings"
)

// call is one command sent to the fake runner.
type call struct {
	Cmd   string
	Stdin string
}

// fakeRunner answers commands with a handler; handler output is split into
// lines for Stream.
type fakeRunner struct {
	calls   []call
	handler func(c call) (string, error)
}

func (f *fakeRunner) Run(_ context.Context, cmd string, stdin []byte) ([]byte, error) {
	c := call{cmd, string(stdin)}
	f.calls = append(f.calls, c)
	out, err := f.handler(c)
	return []byte(out), err
}

func (f *fakeRunner) Stream(ctx context.Context, cmd string, stdin []byte, onLine func(string)) error {
	out, err := f.Run(ctx, cmd, stdin)
	for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if l != "" {
			onLine(l)
		}
	}
	return err
}

// scriptIs reports whether the call runs the embedded script name.
func scriptIs(c call, name string) bool {
	return strings.Contains(c.Stdin, "# wayveil-script: "+name)
}
