package ownerstore

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestSaveLoad(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	p := ServerProfile{Host: "2001:db8::1", Port: 22, User: "root", HostKey: "ssh-ed25519 AAAA",
		PrivateKeyPEM: []byte("-----BEGIN-----\nx\n"), AuthorizedLine: "ssh-ed25519 BBBB wayveil"}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Load("2001:db8::1", 22)
	if err != nil || !reflect.DeepEqual(got, p) {
		t.Fatalf("%+v %v", got, err)
	}
	dir, _ := Dir()
	files, _ := filepath.Glob(filepath.Join(dir, "servers", "*.json"))
	if len(files) != 1 {
		t.Fatalf("files %v", files)
	}
	raw, _ := os.ReadFile(files[0])
	if strings.Contains(strings.ToLower(string(raw)), "password") {
		t.Fatal("profile must not contain a password field")
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(files[0]); st.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", st.Mode().Perm())
		}
	}
	if err := Delete("2001:db8::1", 22); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("2001:db8::1", 22); !os.IsNotExist(err) {
		t.Fatalf("after delete: %v", err)
	}
}
