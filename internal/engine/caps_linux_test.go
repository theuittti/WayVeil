package engine

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theuittti/WayVeil/internal/singbox"
)

func copyBin(t *testing.T) string {
	t.Helper()
	src, err := singbox.Find()
	if err != nil {
		t.Skip("run make singbox")
	}
	dst := filepath.Join(t.TempDir(), "sing-box")
	in, _ := os.Open(src)
	defer in.Close()
	out, _ := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0o755)
	io.Copy(out, in)
	out.Close()
	return dst
}

func TestHasCapsFalse(t *testing.T) {
	ok, err := HasCaps(copyBin(t))
	if err != nil || ok {
		t.Fatalf("%v %v", ok, err)
	}
}

func capBlob(permitted uint32) []byte {
	b := make([]byte, 20)
	binary.LittleEndian.PutUint32(b[0:], 0x02000001) // VFS_CAP_REVISION_2 | effective
	binary.LittleEndian.PutUint32(b[4:], permitted)
	return b
}

func TestHasCapsParsesXattr(t *testing.T) {
	if !parseCapXattr(capBlob(1<<12 | 1<<10)) {
		t.Fatal("bit 12 set → true")
	}
	if parseCapXattr(capBlob(1 << 10)) {
		t.Fatal("bit 12 clear → false")
	}
	if parseCapXattr([]byte{1, 2}) {
		t.Fatal("short blob → false")
	}
}

func TestGrantCapsCommand(t *testing.T) {
	dir := t.TempDir()
	argv := filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argv + "\nexit 126\n"
	os.WriteFile(filepath.Join(dir, "pkexec"), []byte(script), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	err := GrantCaps(bg, "/opt/x/sing-box")
	if !errors.Is(err, ErrElevationDenied) {
		t.Fatalf("%v", err)
	}
	got, _ := os.ReadFile(argv)
	if strings.TrimSpace(string(got)) != "setcap\ncap_net_admin,cap_net_bind_service,cap_net_raw+ep\n/opt/x/sing-box" {
		t.Fatalf("argv %q", got)
	}
}

func TestConnectNeedsElevation(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root needs no capabilities")
	}
	s, o := setup(t)
	t.Setenv("WAYVEIL_SINGBOX", copyBin(t))
	o.Inbound = singbox.TUN
	e, _ := New(o)
	if err := e.Connect(bg, s); !errors.Is(err, ErrNeedsElevation) {
		t.Fatalf("%v", err)
	}
	if _, ok := e.readRun(); ok {
		t.Fatal("process must not be started")
	}
}
