package server

import (
	"context"

	"github.com/wayveil/wayveil/internal/remote"
)

// Uninstall removes everything recorded in the server manifest and, last,
// the app's own key line from authorized_keys. It is idempotent.
func Uninstall(ctx context.Context, r remote.Runner, authKeyLine string, onStep func(StepEvent)) error {
	_, err := runScript(ctx, r, "uninstall", map[string]string{"WV_AUTH_KEY": authKeyLine}, onStep, nil)
	return err
}

// uploadUninstaller leaves a copy of the uninstaller on the server so it can
// be run by hand without the app.
func uploadUninstaller(ctx context.Context, r remote.Runner) error {
	_, err := r.Run(ctx, "echo 'file /etc/xray/wayveil-uninstall.sh' >> /etc/xray/wayveil-manifest && "+
		"install -m 0700 /dev/stdin /etc/xray/wayveil-uninstall.sh", script("uninstall"))
	return err
}
