package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/wayveil/wayveil/internal/targetcheck"
	"github.com/wayveil/wayveil/internal/targetscan"
)

func TestCheckJSON(t *testing.T) {
	var out bytes.Buffer
	d := deps{check: func(_ context.Context, domain string, _ targetcheck.Options) targetcheck.Report {
		return targetcheck.Report{Domain: domain, OK: true, Problems: []targetcheck.Problem{}}
	}}
	if code := run([]string{"check", "-domain", "x"}, &out, d); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var r map[string]any
	if err := json.Unmarshal(out.Bytes(), &r); err != nil || r["domain"] != "x" {
		t.Fatalf("%s %v", out.String(), err)
	}
}

func TestScanJSONLines(t *testing.T) {
	var out bytes.Buffer
	d := deps{scan: func(_ context.Context, o targetscan.Options, found func(targetscan.Candidate)) (int, error) {
		if o.Self.String() != "198.51.100.7" || o.Limit != 3 {
			t.Errorf("opts %+v", o)
		}
		found(targetscan.Candidate{Domain: "a.example", IP: "198.51.100.8"})
		return 42, nil
	}}
	if code := run([]string{"scan", "-self", "198.51.100.7", "-limit", "3"}, &out, d); code != 0 {
		t.Fatalf("exit %d", code)
	}
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("%q", out.String())
	}
	var a, b map[string]any
	json.Unmarshal(lines[0], &a)
	json.Unmarshal(lines[1], &b)
	if a["type"] != "found" || a["domain"] != "a.example" || b["type"] != "done" || b["scanned"] != float64(42) {
		t.Fatalf("%v %v", a, b)
	}
}
