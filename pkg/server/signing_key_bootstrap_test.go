package server

import (
	"testing"

	"prohibitorum/pkg/configx"
)

func TestSigningKeyBootstrap_CurrentDEK(t *testing.T) {
	if _, _, ok := currentDEK(&configx.Config{}); ok {
		t.Fatal("empty key set should return ok=false")
	}
	cfg := &configx.Config{DataEncryptionKeys: map[int][]byte{1: []byte("aaaa"), 3: []byte("cccc"), 2: []byte("bbbb")}}
	ver, key, ok := currentDEK(cfg)
	if !ok || ver != 3 || string(key) != "cccc" {
		t.Fatalf("currentDEK = (%d, %q, %v), want (3, cccc, true)", ver, key, ok)
	}
}
