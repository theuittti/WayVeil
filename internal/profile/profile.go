// Package profile is a client connection profile imported from a share link.
package profile

import "github.com/theuittti/WayVeil/internal/link"

type Profile struct {
	Name   string      `json:"name"`
	Link   string      `json:"link"`
	Params link.Params `json:"params"`
}
