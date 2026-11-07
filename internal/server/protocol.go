package server

import (
	"context"
	"embed"
	"encoding/json"
	"strings"

	"github.com/wayveil/wayveil/internal/remote"
)

//go:embed scripts/*.sh assets/*
var files embed.FS

type StepStatus string

const (
	StepStart StepStatus = "start"
	StepOK    StepStatus = "ok"
	StepWarn  StepStatus = "warn"
	StepFail  StepStatus = "fail"
)

type StepEvent struct {
	ID      string
	Status  StepStatus
	Message string
}

// ScriptError is a ##ERROR line from a server script; Error() is its code.
type ScriptError struct{ Code, Message string }

func (e *ScriptError) Error() string { return e.Code }

// Is matches ScriptErrors by code, so errors.Is(err, ErrAlreadyInstalled) works.
func (e *ScriptError) Is(target error) bool {
	t, ok := target.(*ScriptError)
	return ok && t.Code == e.Code
}

type Line struct {
	Step   *StepEvent
	Result json.RawMessage
	Err    *ScriptError
	Raw    string
}

// ParseLine decodes one line of the server script output protocol.
func ParseLine(s string) Line {
	l := Line{Raw: s}
	switch {
	case strings.HasPrefix(s, "##STEP "):
		f := strings.SplitN(strings.TrimPrefix(s, "##STEP "), " ", 3)
		if len(f) >= 2 {
			ev := StepEvent{ID: f[0], Status: StepStatus(f[1])}
			if len(f) == 3 {
				ev.Message = f[2]
			}
			l.Step = &ev
		}
	case strings.HasPrefix(s, "##RESULT "):
		l.Result = json.RawMessage(strings.TrimPrefix(s, "##RESULT "))
	case strings.HasPrefix(s, "##ERROR "):
		f := strings.SplitN(strings.TrimPrefix(s, "##ERROR "), " ", 2)
		e := &ScriptError{Code: f[0]}
		if len(f) == 2 {
			e.Message = f[1]
		}
		l.Err = e
	}
	return l
}

func script(name string) []byte {
	b, err := files.ReadFile("scripts/" + name + ".sh")
	if err != nil {
		panic("missing embedded script " + name)
	}
	return b
}

// runScript pipes the embedded script to `bash -s -- args...` as root and
// returns its ##RESULT payload. A ##ERROR line wins over the exit status.
func runScript(ctx context.Context, r remote.Runner, name string, env map[string]string,
	onStep func(StepEvent), onRaw func(string), args ...string) (json.RawMessage, error) {
	cmd := "bash -s --"
	for _, a := range args {
		cmd += " " + remote.ShellQuote(a)
	}
	var result json.RawMessage
	var scriptErr *ScriptError
	err := r.Stream(ctx, cmd, remote.WithEnv(script(name), env), func(s string) {
		l := ParseLine(s)
		switch {
		case l.Step != nil:
			if onStep != nil {
				onStep(*l.Step)
			}
		case l.Result != nil:
			result = l.Result
		case l.Err != nil:
			scriptErr = l.Err
		default:
			if onRaw != nil {
				onRaw(s)
			}
		}
	})
	if scriptErr != nil {
		return nil, scriptErr
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}
