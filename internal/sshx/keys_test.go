package sshx

import (
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateKey(t *testing.T) {
	pemBytes, line, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "ssh-ed25519 ") || !strings.HasSuffix(line, " wayveil") {
		t.Fatalf("line %q", line)
	}
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		t.Fatal(err)
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil || string(pub.Marshal()) != string(signer.PublicKey().Marshal()) {
		t.Fatalf("public key mismatch: %v", err)
	}
}

func TestWrapCommand(t *testing.T) {
	if got := wrap("root", "id -u"); got != "id -u" {
		t.Fatalf("root: %q", got)
	}
	if got := wrap("deploy", "echo 'a'"); got != `sudo -n bash -c 'echo '\''a'\'''` {
		t.Fatalf("deploy: %q", got)
	}
}
