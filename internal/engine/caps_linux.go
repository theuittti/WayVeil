//go:build linux

package engine

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

const capNetAdmin = 12

// HasCaps reports whether bin has CAP_NET_ADMIN in its permitted file capabilities.
func HasCaps(bin string) (bool, error) {
	buf := make([]byte, 64)
	n, err := unix.Getxattr(bin, "security.capability", buf)
	if errors.Is(err, unix.ENODATA) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return parseCapXattr(buf[:n]), nil
}

// parseCapXattr reads struct vfs_cap_data: magic_etc, then {permitted, inheritable} words.
func parseCapXattr(b []byte) bool {
	if len(b) < 12 {
		return false
	}
	return binary.LittleEndian.Uint32(b[4:8])&(1<<capNetAdmin) != 0
}

// GrantCaps asks once (polkit dialog) to let sing-box create the TUN device.
// Nothing else ever runs as root.
func GrantCaps(ctx context.Context, bin string) error {
	cmd := exec.CommandContext(ctx, "pkexec", "setcap", "cap_net_admin,cap_net_bind_service,cap_net_raw+ep", bin)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if errors.As(err, &ee) && (ee.ExitCode() == 126 || ee.ExitCode() == 127) {
		return ErrElevationDenied
	}
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%w: pkexec not found; run: sudo setcap cap_net_admin,cap_net_bind_service,cap_net_raw+ep %s", ErrElevationDenied, bin)
	}
	if err != nil {
		return fmt.Errorf("setcap: %v: %s", err, out)
	}
	return nil
}

func needsCaps(bin string) (bool, error) {
	if os.Geteuid() == 0 {
		return false, nil
	}
	ok, err := HasCaps(bin)
	return !ok, err
}
