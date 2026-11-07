package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wayveil/wayveil/internal/link"
)

func ctlRunner(t *testing.T, reply string, seen *call) *fakeRunner {
	return &fakeRunner{handler: func(c call) (string, error) {
		if !scriptIs(c, "ctl") {
			t.Errorf("unexpected script")
		}
		*seen = c
		return reply + "\n", nil
	}}
}

func TestAddClientEncodesName(t *testing.T) {
	var c call
	f := ctlRunner(t, `##RESULT {"client":{"id":"c-1a2b3c4d","uuid":"u","name":"Вася ноутбук","created":"2026-11-07T12:00:00Z"}}`, &c)
	got, err := AddClient(context.Background(), f, "Вася ноутбук")
	if err != nil {
		t.Fatal(err)
	}
	if c.Cmd != "bash -s -- 'add' '0JLQsNGB0Y8g0L3QvtGD0YLQsdGD0Lo='" {
		t.Fatalf("cmd %q", c.Cmd)
	}
	if got.Name != "Вася ноутбук" || got.ID != "c-1a2b3c4d" || !got.Created.Equal(time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("%+v", got)
	}
}

func TestRemoveUnknownClient(t *testing.T) {
	var c call
	f := ctlRunner(t, `##ERROR no_such_client c-0`, &c)
	err := RemoveClient(context.Background(), f, "c-0")
	var se *ScriptError
	if !errors.As(err, &se) || se.Code != "no_such_client" {
		t.Fatalf("%v", err)
	}
}

func TestLastSeenParses(t *testing.T) {
	var c call
	f := ctlRunner(t, `##RESULT {"lastSeen":{"c-1a2b3c4d":{"ip":"95.24.1.2","at":"2026-11-07T12:00:00Z"}}}`, &c)
	m, err := LastSeen(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	s := m["c-1a2b3c4d"]
	if s.IP != "95.24.1.2" || s.At.Year() != 2026 {
		t.Fatalf("%+v", m)
	}
}

func TestTrafficParses(t *testing.T) {
	var c call
	f := ctlRunner(t, `##RESULT {"traffic":{"c-1":{"up":10,"down":2000}}}`, &c)
	m, err := TrafficAll(context.Background(), f)
	if err != nil || m["c-1"] != (Traffic{Up: 10, Down: 2000}) {
		t.Fatalf("%+v %v", m, err)
	}
}

func TestClientLink(t *testing.T) {
	info := ServerInfo{Address: "203.0.113.10", SNI: "www.nu.nl", PublicKey: "PBK", ShortID: "ab12"}
	cl := Client{ID: "c-1", UUID: "uuid-1", Name: `O'Brien "x"`}
	want := link.Build(link.Params{UUID: "uuid-1", Host: "203.0.113.10", SNI: "www.nu.nl", PublicKey: "PBK", ShortID: "ab12", Name: `O'Brien "x"`})
	if got := ClientLink(info, cl); got != want || !strings.HasPrefix(got, "vless://uuid-1@") {
		t.Fatalf("%s", got)
	}
}
