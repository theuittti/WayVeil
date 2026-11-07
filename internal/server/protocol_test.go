package server

import (
	"context"
	"testing"
)

func TestParseLine(t *testing.T) {
	l := ParseLine("##STEP xray ok")
	if l.Step == nil || *l.Step != (StepEvent{"xray", "ok", ""}) {
		t.Fatalf("%+v", l)
	}
	l = ParseLine("##STEP bbr warn sysctl not permitted")
	if l.Step == nil || l.Step.Message != "sysctl not permitted" || l.Step.Status != "warn" {
		t.Fatalf("%+v", l)
	}
	l = ParseLine(`##RESULT {"a":1}`)
	if string(l.Result) != `{"a":1}` {
		t.Fatalf("%+v", l)
	}
	l = ParseLine("##ERROR port_busy nginx on 443")
	if l.Err == nil || l.Err.Code != "port_busy" || l.Err.Message != "nginx on 443" {
		t.Fatalf("%+v", l)
	}
	l = ParseLine("hello")
	if l.Step != nil || l.Result != nil || l.Err != nil || l.Raw != "hello" {
		t.Fatalf("%+v", l)
	}
}

func TestPrecheckParsesResult(t *testing.T) {
	f := &fakeRunner{handler: func(c call) (string, error) {
		if !scriptIs(c, "precheck") {
			t.Errorf("unexpected call %q", c.Cmd)
		}
		return `##RESULT {"osId":"ubuntu","osVersion":"24.04","arch":"amd64","supported":true,"port443Busy":false,"port443Process":"","hasWayveil":false,"hasForeignXray":false,"publicIp":"203.0.113.10"}` + "\n", nil
	}}
	h, err := Precheck(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	want := HostInfo{OSID: "ubuntu", OSVersion: "24.04", Arch: "amd64", Supported: true, PublicIP: "203.0.113.10"}
	if h != want {
		t.Fatalf("%+v", h)
	}
}

func TestRunScriptReportsScriptError(t *testing.T) {
	f := &fakeRunner{handler: func(call) (string, error) {
		return "##STEP deps start\n##ERROR install_failed apt broke\n", nil
	}}
	_, err := runScript(context.Background(), f, "precheck", nil, nil, nil)
	se, ok := err.(*ScriptError)
	if !ok || se.Code != "install_failed" {
		t.Fatalf("%v", err)
	}
}
