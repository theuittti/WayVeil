package server

import (
	"context"

	"github.com/theuittti/WayVeil/internal/remote"
)

// Uninstall removes everything recorded in the server manifest and, last,
// the app's own key line from authorized_keys. It is idempotent.
func Uninstall(ctx context.Context, r remote.Runner, authKeyLine string, onStep func(StepEvent)) error {
	return uninstallWith(ctx, r, authKeyLine, onStep, nil)
}

func uninstallWith(ctx context.Context, r remote.Runner, authKeyLine string, onStep func(StepEvent), extra map[string]string) error {
	env := map[string]string{"WV_AUTH_KEY": authKeyLine}
	for k, v := range extra {
		env[k] = v
	}
	_, err := runScript(ctx, r, "uninstall", env, onStep, nil)
	return err
}
