package pat

import (
	"crypto/sha256"
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	raw, hash, hint, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasPrefix(raw, Prefix) {
		t.Errorf("raw missing prefix: %q", raw)
	}
	want := sha256.Sum256([]byte(raw))
	if string(hash) != string(want[:]) {
		t.Error("hash != sha256(raw)")
	}
	if !strings.HasPrefix(hint, Prefix) || !strings.HasSuffix(hint, raw[len(raw)-4:]) {
		t.Errorf("hint format wrong: %q", hint)
	}
	raw2, _, _, _ := Generate()
	if raw == raw2 {
		t.Error("two Generate() calls must differ")
	}
}

func TestHashTokenDeterministic(t *testing.T) {
	raw, hash, _, _ := Generate()
	if string(HashToken(raw)) != string(hash) {
		t.Error("HashToken(raw) != Generate hash")
	}
}

func TestHintShortInput(t *testing.T) {
	if got := Hint("abc"); got != Prefix+"…abc" {
		t.Errorf("Hint(short) = %q", got)
	}
}
