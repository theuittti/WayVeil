package engine

import (
	"errors"

	"github.com/theuittti/WayVeil/internal/mihomo"
)

var (
	// ErrNeedsElevation: mihomo lacks the capability to create the TUN
	// device; call GrantCaps once, then Connect again.
	ErrNeedsElevation  = errors.New("needs_elevation")
	ErrElevationDenied = errors.New("elevation_denied")
)

func (e *Engine) preflight(bin string) error {
	if e.o.Inbound != mihomo.TUN {
		return nil
	}
	need, err := needsCaps(bin)
	if err != nil {
		return err
	}
	if need {
		return ErrNeedsElevation
	}
	return nil
}
