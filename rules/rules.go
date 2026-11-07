// Package rules holds data files shipped with the app, such as preset
// REALITY targets.
package rules

import (
	_ "embed"
	"encoding/json"
)

//go:embed targets.json
var targetsJSON []byte

type Targets struct {
	Version   int                 `json:"version"`
	ByCountry map[string][]string `json:"byCountry"`
	Russian   []string            `json:"russian"`
}

// DefaultTargets returns the preset targets bundled with the app.
func DefaultTargets() Targets {
	var t Targets
	if err := json.Unmarshal(targetsJSON, &t); err != nil {
		panic("rules/targets.json: " + err.Error())
	}
	return t
}
