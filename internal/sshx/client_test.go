package sshx

import (
	"context"
	"testing"
	"time"
)

func TestConnectUnreachableTimesOut(t *testing.T) {
	start := time.Now()
	_, err := Connect(context.Background(), Target{Host: "192.0.2.1", Port: 22, Password: "x", DialTimeout: 2 * time.Second})
	if err == nil {
		t.Fatal("expected error")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v", d)
	}
}
