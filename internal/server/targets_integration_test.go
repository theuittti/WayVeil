//go:build integration

package server

import (
	"slices"
	"testing"

	"github.com/wayveil/wayveil/internal/targetcheck"
	"github.com/wayveil/wayveil/internal/testenv"
	"github.com/wayveil/wayveil/rules"
)

func TestCheckTargetReal(t *testing.T) {
	root := testenv.Start(t).Root(t)
	rep, err := CheckTarget(bg, root, "amd64", "www.microsoft.com")
	if err != nil || !rep.TLS13 {
		t.Fatalf("%+v %v", rep, err)
	}
	rep, err = CheckTarget(bg, root, "amd64", "tls-v1-2.badssl.com")
	if err != nil || !slices.Contains(rep.Problems, targetcheck.NoTLS13) {
		t.Fatalf("%+v %v", rep, err)
	}
	if err := CleanupScanner(bg, root); err != nil {
		t.Fatal(err)
	}
	if testenv.Sh(t, root, "test -e /tmp/wv-scan && echo left || echo gone") != "gone" {
		t.Fatal("scanner left on server")
	}
}

func TestSuggestTargetsReal(t *testing.T) {
	root := testenv.Start(t).Root(t)
	var got []Suggestion
	h := HostInfo{Arch: "amd64"} // no public IP: presets only, no scan
	if err := SuggestTargets(bg, root, h, rules.DefaultTargets(), func(s Suggestion) { got = append(got, s) }); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(got, func(s Suggestion) bool { return s.Kind == KindRussian && s.Report.OK }) {
		t.Fatalf("no verified russian preset: %+v", got)
	}
	if testenv.Sh(t, root, "test -e /tmp/wv-scan && echo left || echo gone") != "gone" {
		t.Fatal("scanner left on server")
	}
}
