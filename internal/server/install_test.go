package server

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func precheckJSON(busy, hasWV bool) string {
	return fmt.Sprintf(`##RESULT {"osId":"ubuntu","osVersion":"24.04","arch":"amd64","supported":true,"port443Busy":%v,"port443Process":"nginx","hasWayveil":%v,"hasForeignXray":false,"publicIp":"203.0.113.10"}`+"\n", busy, hasWV)
}

var installOut = strings.Join([]string{
	"##STEP uninstaller ok", "##STEP authkey ok", "##STEP deps start", "##STEP deps ok", "##STEP xray ok",
	"##STEP keys ok", "##STEP config ok", "##STEP service ok", "##STEP bbr warn sysctl not permitted",
	"##STEP logrotate ok", "##STEP verify ok",
	`##RESULT {"publicKey":"PBK","shortId":"ab12cd34","xrayVersion":"v26.10.31"}`,
}, "\n") + "\n"

func installRunner(t *testing.T, busy, hasWV bool) *fakeRunner {
	return &fakeRunner{handler: func(c call) (string, error) {
		switch {
		case scriptIs(c, "precheck"):
			return precheckJSON(busy, hasWV), nil
		case scriptIs(c, "install"):
			return installOut, nil
		}
		return "", nil
	}}
}

var opts = InstallOptions{Address: "203.0.113.10", SNI: "www.nu.nl", AuthUser: "root", AuthKeyLine: "ssh-ed25519 AAAA wayveil"}

func sentInstall(f *fakeRunner) bool {
	return slices.ContainsFunc(f.calls, func(c call) bool { return scriptIs(c, "install") })
}

func TestInstallRefusesBusyPort(t *testing.T) {
	f := installRunner(t, true, false)
	_, err := Install(context.Background(), f, opts, nil)
	var se *ScriptError
	if !errors.As(err, &se) || se.Code != "port_busy" || !strings.Contains(se.Message, "nginx") {
		t.Fatalf("%v", err)
	}
	if sentInstall(f) {
		t.Fatal("install script must not run")
	}
}

func TestInstallRefusesAlreadyInstalled(t *testing.T) {
	f := installRunner(t, false, true)
	_, err := Install(context.Background(), f, opts, nil)
	if !errors.Is(err, ErrAlreadyInstalled) || sentInstall(f) {
		t.Fatalf("%v", err)
	}
}

func TestInstallHappyPath(t *testing.T) {
	f := installRunner(t, false, false)
	var ids []string
	res, err := Install(context.Background(), f, opts, func(e StepEvent) {
		if e.Status != StepStart {
			ids = append(ids, e.ID)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"uninstaller", "authkey", "deps", "xray", "keys", "config", "service", "bbr", "logrotate", "verify"}
	if !slices.Equal(ids, want) {
		t.Fatalf("steps %v", ids)
	}
	if res.PublicKey != "PBK" || res.ShortID != "ab12cd34" || len(res.Warnings) != 1 || res.Warnings[0].ID != "bbr" {
		t.Fatalf("%+v", res)
	}
	var env string
	for _, c := range f.calls {
		if scriptIs(c, "install") {
			env = c.Stdin
		}
	}
	for _, kv := range []string{"export WV_SNI='www.nu.nl'", "export WV_DNS='1.1.1.1'", "export WV_XRAY_VERSION='" + XrayVersion + "'", "export WV_AUTH_USER='root'"} {
		if !strings.Contains(env, kv) {
			t.Errorf("missing %s", kv)
		}
	}
}
