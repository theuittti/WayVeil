package clientstore

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/theuittti/WayVeil/internal/appdir"
	"github.com/theuittti/WayVeil/internal/ownerstore"
)

func TestOwnerstoreUsesAppdir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a, _ := appdir.ConfigDir()
	o, _ := ownerstore.Dir()
	if a != o {
		t.Fatalf("%s != %s", a, o)
	}
}

func TestNormalizeDomain(t *testing.T) {
	ok := map[string]string{
		"https://Example.COM/path?q": "example.com",
		" youtube.com ":              "youtube.com",
		"*.site.ru":                  "site.ru",
		"site.ru.":                   "site.ru",
		"site.ru:8443":               "site.ru",
		"пример.рф":                  "xn--e1afmkfd.xn--p1ai",
	}
	for in, want := range ok {
		got, err := NormalizeDomain(in)
		if err != nil || got != want {
			t.Errorf("NormalizeDomain(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"localhost", "", "http://", "a b.com"} {
		if _, err := NormalizeDomain(bad); !errors.Is(err, ErrBadDomain) {
			t.Errorf("NormalizeDomain(%q) err = %v, want ErrBadDomain", bad, err)
		}
	}
}

func TestSettingsDefaultsAndRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s, err := Load()
	if err != nil || s.Mode != ModeSmart || len(s.AlwaysProxy) != 0 || s.Profile != nil {
		t.Fatalf("%+v %v", s, err)
	}
	s.Mode = ModeAll
	s.AlwaysDirect = []string{"gosuslugi.ru"}
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("%+v %v", got, err)
	}
	d, _ := appdir.ConfigDir()
	if st, _ := os.Stat(filepath.Join(d, "settings.json")); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}

func TestAddRuleMovesBetweenLists(t *testing.T) {
	var s Settings
	if err := s.AddRule(&s.AlwaysProxy, "YouTube.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddRule(&s.AlwaysProxy, "youtube.com"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.AlwaysProxy, []string{"youtube.com"}) {
		t.Fatalf("%v", s.AlwaysProxy)
	}
	if err := s.AddRule(&s.AlwaysDirect, "https://youtube.com/"); err != nil {
		t.Fatal(err)
	}
	if len(s.AlwaysProxy) != 0 || !reflect.DeepEqual(s.AlwaysDirect, []string{"youtube.com"}) {
		t.Fatalf("proxy %v direct %v", s.AlwaysProxy, s.AlwaysDirect)
	}
	if err := s.AddRule(&s.AlwaysDirect, "localhost"); !errors.Is(err, ErrBadDomain) {
		t.Fatalf("%v", err)
	}
}
