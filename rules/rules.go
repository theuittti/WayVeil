// Package rules holds data files shipped with the app, such as preset
// REALITY targets.
package rules

import (
	"embed"
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

//go:embed sources.json
var sourcesJSON []byte

//go:embed lists/*.srs
var bundled embed.FS

// Source is where a sing-box rule-set is downloaded from.
type Source struct {
	Tag string `json:"tag"`
	URL string `json:"url"`
}

// DefaultSources returns the rule-set sources bundled with the app.
func DefaultSources() []Source {
	var s []Source
	if err := json.Unmarshal(sourcesJSON, &s); err != nil {
		panic("rules/sources.json: " + err.Error())
	}
	return s
}

// Bundled returns the bundled copy of a rule-set by tag.
func Bundled(tag string) ([]byte, bool) {
	b, err := bundled.ReadFile("lists/" + tag + ".srs")
	return b, err == nil
}
