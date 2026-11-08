package lists

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/theuittti/WayVeil/rules"
)

var tags = []string{"ru-blocked", "telegram-domain", "telegram-ip", "meta-domain", "meta-ip", "twitter-domain", "twitter-ip", "discord-domain", "discord-ip"}

// mrs builds a real (zstd-compressed) mihomo rule-set with the right header.
func mrs(behavior byte, payload string) []byte {
	var buf bytes.Buffer
	w, _ := zstd.NewWriter(&buf)
	w.Write(append([]byte{'M', 'R', 'S', 1, behavior, 0, 0, 0}, payload...))
	w.Close()
	return buf.Bytes()
}

func behaviorOf(tag string) byte {
	if strings.HasSuffix(tag, "-ip") {
		return 1
	}
	return 0
}

func srs(tag string) []byte { return mrs(behaviorOf(tag), "fresh-"+tag) }

// server serves sources.json pointing at itself; bad maps tag → bogus body.
func server(t *testing.T, bad map[string]string) (*httptest.Server, Options) {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sources.json":
			fmt.Fprintf(w, `[{"tag":"ru-blocked","behavior":"domain","url":"https://%s/x.mrs"},
				{"tag":"../../escape","behavior":"domain","url":"https://%s/x.mrs"},
				{"tag":"telegram-ip","behavior":"ipcidr","url":"http://%s/plain.mrs"}]`, r.Host, r.Host, r.Host)
		case "/x.mrs":
			w.Write(mrs(0, "override"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	o := Options{Dir: t.TempDir(), HTTP: srv.Client(), SourcesURL: srv.URL + "/sources.json"}
	res, _ := Update(context.Background(), o)
	if !slices.Contains(res.Updated, "ru-blocked") {
		t.Fatalf("%+v", res)
	}
	paths, _ := Ensure(o)
	if b, _ := os.ReadFile(paths["ru-blocked"]); !bytes.Equal(b, mrs(0, "override")) || len(paths) != 9 {
		t.Fatalf("%q %v", b, paths)
	}
	// unknown tags never become paths; plain-http URLs are not trusted
	if _, err := os.Stat(filepath.Join(o.Dir, "..", "..", "escape.mrs")); err == nil {
		t.Fatal("remote tag escaped the lists dir")
	}
	if slices.Contains(res.Updated, "telegram-ip") && res.Errors["telegram-ip"] == "" {
		t.Fatal("http URL from remote sources must be ignored")
	}
}

func TestValid(t *testing.T) {
	good, _ := rules.Bundled("ru-blocked")
	if !Valid(good, "domain") {
		t.Fatal("bundled ru-blocked must be valid")
	}
	if Valid(good, "ipcidr") {
		t.Fatal("behavior mismatch must be invalid")
	}
	if Valid(good[:len(good)/2], "domain") {
		t.Fatal("truncated file (zstd magic intact) must be invalid")
	}
	if Valid([]byte("<html>"), "domain") || Valid(mrs(0, "x")[:3], "domain") {
		t.Fatal("garbage must be invalid")
	}
	notMRS := mrs(0, "")
	var buf bytes.Buffer
	w, _ := zstd.NewWriter(&buf)
	w.Write([]byte("hello world"))
	w.Close()
	if Valid(buf.Bytes(), "domain") || !Valid(notMRS, "domain") {
		t.Fatal("zstd without MRS header must be invalid")
	}
}

func TestEnsureReplacesTruncatedCache(t *testing.T) {
	o := Options{Dir: t.TempDir()}
	paths, _ := Ensure(o)
	good, _ := os.ReadFile(paths["ru-blocked"])
	os.WriteFile(paths["ru-blocked"], good[:len(good)/2], 0o600)
	Ensure(o)
	if b, _ := os.ReadFile(paths["ru-blocked"]); !bytes.Equal(b, good) {
		t.Fatal("truncated cached list was not replaced by the bundled copy")
	}
}
