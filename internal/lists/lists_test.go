package lists

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/theuittti/WayVeil/rules"
)

var tags = []string{"ru-blocked", "telegram-domain", "telegram-ip", "meta-domain", "meta-ip", "twitter-domain", "twitter-ip", "discord-domain", "discord-ip"}

func srs(tag string) []byte { return append([]byte{0x28, 0xb5, 0x2f, 0xfd}, "fresh-"+tag...) }

// server serves sources.json pointing at itself; bad maps tag → bogus body.
func server(t *testing.T, bad map[string]string) (*httptest.Server, Options) {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sources.json" {
			var parts []string
			for _, tg := range tags {
				parts = append(parts, fmt.Sprintf(`{"tag":%q,"behavior":"domain","url":"%s/%s.mrs"}`, tg, srv.URL, tg))
			}
			fmt.Fprint(w, "["+strings.Join(parts, ",")+"]")
			return
		}
		tag := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".mrs")
		if b, ok := bad[tag]; ok {
			fmt.Fprint(w, b)
			return
		}
		w.Write(srs(tag))
	}))
	t.Cleanup(srv.Close)
	return srv, Options{Dir: t.TempDir(), HTTP: srv.Client(), SourcesURL: srv.URL + "/sources.json"}
}

func TestEnsureWritesBundled(t *testing.T) {
	o := Options{Dir: t.TempDir()}
	paths, err := Ensure(o)
	if err != nil || len(paths) != 9 {
		t.Fatalf("%v %v", paths, err)
	}
	for _, tg := range tags {
		b, _ := os.ReadFile(paths[tg])
		want, ok := rules.Bundled(tg)
		if !ok || !bytes.Equal(b, want) {
			t.Fatalf("%s not bundled copy", tg)
		}
	}
}

func TestUpdateReplacesValid(t *testing.T) {
	_, o := server(t, nil)
	res, err := Update(context.Background(), o)
	if err != nil || len(res.Updated) != 9 {
		t.Fatalf("%+v %v", res, err)
	}
	paths, _ := Ensure(o)
	for _, tg := range tags {
		if b, _ := os.ReadFile(paths[tg]); !bytes.Equal(b, srs(tg)) {
			t.Fatalf("%s = %q", tg, b)
		}
	}
}

func TestUpdateRejectsHTML(t *testing.T) {
	_, o := server(t, map[string]string{"telegram-domain": "<html>rate limited</html>"})
	before, _ := Ensure(o)
	old, _ := os.ReadFile(before["telegram-domain"])
	res, _ := Update(context.Background(), o)
	if !slices.Contains(res.Kept, "telegram-domain") || res.Errors["telegram-domain"] == "" {
		t.Fatalf("%+v", res)
	}
	if b, _ := os.ReadFile(before["telegram-domain"]); !bytes.Equal(b, old) {
		t.Fatal("telegram file replaced by invalid download")
	}
}

type downTransport struct{}

func (downTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("network down")
}

func TestUpdateServerDown(t *testing.T) {
	_, o := server(t, nil)
	o.HTTP = &http.Client{Transport: downTransport{}} // GitHub unreachable too
	res, _ := Update(context.Background(), o)
	if len(res.Updated) != 0 || len(res.Kept) != 9 {
		t.Fatalf("%+v", res)
	}
	if paths, err := Ensure(o); err != nil || len(paths) != 9 {
		t.Fatalf("%v %v", paths, err)
	}
}

func TestSourcesOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sources.json":
			fmt.Fprintf(w, `[{"tag":"ru-blocked","behavior":"domain","url":"http://%s/x.mrs"}]`, r.Host)
		case "/x.mrs":
			w.Write([]byte("\x28\xb5\x2f\xfdoverride"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	o := Options{Dir: t.TempDir(), HTTP: srv.Client(), SourcesURL: srv.URL + "/sources.json"}
	res, _ := Update(context.Background(), o)
	if !slices.Equal(res.Updated, []string{"ru-blocked"}) {
		t.Fatalf("%+v", res)
	}
	paths, _ := Ensure(o)
	if b, _ := os.ReadFile(paths["ru-blocked"]); string(b) != "\x28\xb5\x2f\xfdoverride" || len(paths) != 9 {
		t.Fatalf("%q %v", b, paths)
	}
}

func TestValid(t *testing.T) {
	zst := []byte{0x28, 0xb5, 0x2f, 0xfd}
	if Valid(zst[:3]) || Valid([]byte("<html>")) || Valid([]byte("SRS\x03x")) || !Valid(append(zst, 'x')) || Valid(append(zst, make([]byte, 4<<20)...)) {
		t.Fatal("Valid")
	}
}
