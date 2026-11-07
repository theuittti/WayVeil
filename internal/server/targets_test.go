package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/wayveil/wayveil/rules"
)

var testTargets = rules.Targets{Version: 1,
	ByCountry: map[string][]string{"NL": {"www.nu.nl", "www.bol.com"}, "DE": {"www.spiegel.de"}},
	Russian:   []string{"vk.com", "ozon.ru"}}

func checkLine(d string, ok bool, ms int) string {
	return fmt.Sprintf(`{"domain":%q,"ok":%v,"tls13":%v,"http2":true,"handshakeMs":%d,"problems":[]}`, d, ok, ok, ms)
}

func suggestRunner(nearby bool) *fakeRunner {
	return &fakeRunner{handler: func(c call) (string, error) {
		switch {
		case strings.Contains(c.Cmd, "ipinfo.io"):
			return "NL\n", nil
		case strings.Contains(c.Cmd, "wv-scan check"):
			if strings.Contains(c.Cmd, "www.spiegel.de") {
				panic("other-country preset must not be checked")
			}
			return strings.Join([]string{checkLine("www.nu.nl", true, 40), checkLine("www.bol.com", false, 0),
				checkLine("vk.com", true, 80), checkLine("ozon.ru", false, 0)}, "\n"), nil
		case strings.Contains(c.Cmd, "wv-scan scan"):
			if !strings.Contains(c.Cmd, "-self '203.0.113.10'") {
				panic("scan must use server IP: " + c.Cmd)
			}
			if nearby {
				return `{"type":"found","domain":"nearby.example","ip":"203.0.113.11","report":` + checkLine("nearby.example", true, 5) + "}\n" + `{"type":"done","scanned":1023}`, nil
			}
			return `{"type":"done","scanned":1023}`, nil
		}
		return "", nil
	}}
}

var host = HostInfo{Arch: "amd64", PublicIP: "203.0.113.10", Supported: true}

func collectSuggestions(t *testing.T, f *fakeRunner) []Suggestion {
	var got []Suggestion
	if err := SuggestTargets(context.Background(), f, host, testTargets, func(s Suggestion) { got = append(got, s) }); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSuggestOrder(t *testing.T) {
	got := collectSuggestions(t, suggestRunner(true))
	var order []string
	for _, s := range got {
		order = append(order, fmt.Sprintf("%s:%s:%v", s.Kind, s.Domain, s.Recommended))
	}
	if len(got) != 3 || got[2].Domain != "nearby.example" || got[2].Kind != KindNearby || !got[2].Recommended {
		t.Fatalf("%v", order)
	}
	kinds := map[string]SuggestionKind{got[0].Domain: got[0].Kind, got[1].Domain: got[1].Kind}
	if kinds["www.nu.nl"] != KindCountry || kinds["vk.com"] != KindRussian || got[0].Recommended || got[1].Recommended {
		t.Fatalf("%v", order)
	}
}

func TestSuggestFallbackRecommend(t *testing.T) {
	got := collectSuggestions(t, suggestRunner(false))
	last := got[len(got)-1]
	if last.Domain != "www.nu.nl" || last.Kind != KindCountry || !last.Recommended {
		t.Fatalf("%+v", got)
	}
}

func TestCleanupCalled(t *testing.T) {
	f := suggestRunner(true)
	collectSuggestions(t, f)
	if last := f.calls[len(f.calls)-1]; !strings.Contains(last.Cmd, "rm -f "+scannerPath) {
		t.Fatalf("last call %q", last.Cmd)
	}
	uploads := 0
	for _, c := range f.calls {
		if strings.Contains(c.Cmd, "/dev/stdin "+scannerPath) && len(c.Stdin) > 1000 {
			uploads++
		}
	}
	if uploads != 1 {
		t.Fatalf("scanner uploads: %d", uploads)
	}
}

// Final review I5: root must not execute a scanner from a world-writable dir.
func TestScannerNotInWorldWritableDir(t *testing.T) {
	if strings.HasPrefix(scannerPath, "/tmp/") || strings.HasPrefix(scannerPath, "/var/tmp/") {
		t.Fatalf("scanner path %s is world-writable", scannerPath)
	}
	f := suggestRunner(true)
	collectSuggestions(t, f)
	for _, c := range f.calls {
		if strings.Contains(c.Cmd, "/dev/stdin "+scannerPath) && !strings.Contains(c.Cmd, "install -d -m 0700") {
			t.Fatalf("upload must create a root-only dir: %q", c.Cmd)
		}
	}
}
