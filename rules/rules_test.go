package rules

import (
	"regexp"
	"testing"
)

func TestTargetsValid(t *testing.T) {
	tg := DefaultTargets()
	if tg.Version != 1 || len(tg.ByCountry) == 0 || len(tg.Russian) == 0 {
		t.Fatalf("%+v", tg)
	}
	dom := regexp.MustCompile(`^[a-z0-9.-]+\.[a-z]{2,}$`)
	cc := regexp.MustCompile(`^[A-Z]{2}$`)
	seen := map[string]bool{}
	check := func(d string) {
		if !dom.MatchString(d) {
			t.Errorf("bad domain %q", d)
		}
		if seen[d] {
			t.Errorf("duplicate %q", d)
		}
		seen[d] = true
	}
	for c, list := range tg.ByCountry {
		if !cc.MatchString(c) {
			t.Errorf("bad country %q", c)
		}
		for _, d := range list {
			check(d)
		}
	}
	for _, d := range tg.Russian {
		check(d)
	}
}
