package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"strings"

	"golang.org/x/crypto/ssh"
)

// GenerateKey creates the app's own ed25519 key: an OpenSSH PEM private key
// and the authorized_keys line (comment "wayveil").
func GenerateKey() (privatePEM []byte, authorizedLine string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, "wayveil")
	if err != nil {
		return nil, "", err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, "", err
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " wayveil"
	return pem.EncodeToMemory(block), line, nil
}
