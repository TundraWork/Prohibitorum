package password

import (
	"encoding/base64"
	"testing"

	"prohibitorum/pkg/configx"
)

func TestPHC_RoundTrip(t *testing.T) {
	params := configx.PasswordHashParams{MemoryKiB: 65536, Iterations: 3, Parallelism: 1}
	salt := []byte("16-byte-salt----")
	tag := []byte("32-byte-tag-output---------------")

	s := PHCEncode(params, salt, tag)

	got, err := PHCDecode(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Params != params {
		t.Errorf("params %+v want %+v", got.Params, params)
	}
	if string(got.Salt) != string(salt) {
		t.Errorf("salt mismatch: got %q want %q", got.Salt, salt)
	}
	if string(got.Tag) != string(tag) {
		t.Errorf("tag mismatch")
	}
}

func TestPHC_RejectsMalformed(t *testing.T) {
	bad := []string{
		"",
		"$argon2id$v=19$m=65536,t=3,p=1$only-three-segments",
		"$argon2i$v=19$m=65536,t=3,p=1$" + base64.RawStdEncoding.EncodeToString([]byte("salt")) + "$" + base64.RawStdEncoding.EncodeToString([]byte("tag")),
		"$argon2id$v=18$m=65536,t=3,p=1$xxx$yyy",
		"$argon2id$v=19$m=BAD,t=3,p=1$xxx$yyy",
	}
	for _, b := range bad {
		if _, err := PHCDecode(b); err == nil {
			t.Errorf("PHCDecode(%q) should fail", b)
		}
	}
}

// TestPHC_RejectsBelowFloor verifies the Bundle-3 Crypto Open-Q-5 fix:
// PHCDecode must reject obviously-weak param strings as defense-in-depth
// against tampered/injected stored hashes. The floor is intentionally
// well below the OWASP minimum (production params are 64 MiB / 3
// iterations); it's a sanity check, not a config gate.
func TestPHC_RejectsBelowFloor(t *testing.T) {
	saltB64 := base64.RawStdEncoding.EncodeToString([]byte("16-byte-salt----"))
	tagB64 := base64.RawStdEncoding.EncodeToString([]byte("32-byte-tag-output---------------"))

	cases := []struct {
		name string
		s    string
	}{
		{"memory=4KiB", "$argon2id$v=19$m=4,t=1,p=1$" + saltB64 + "$" + tagB64},
		{"memory=1024KiB", "$argon2id$v=19$m=1024,t=1,p=1$" + saltB64 + "$" + tagB64},
		{"iterations=0", "$argon2id$v=19$m=65536,t=0,p=1$" + saltB64 + "$" + tagB64},
		{"parallelism=0", "$argon2id$v=19$m=65536,t=3,p=0$" + saltB64 + "$" + tagB64},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := PHCDecode(c.s); err == nil {
				t.Errorf("PHCDecode(%q) should fail floor check", c.s)
			}
		})
	}

	// Sanity: a string at exactly the floor must NOT be rejected.
	good := "$argon2id$v=19$m=8192,t=1,p=1$" + saltB64 + "$" + tagB64
	if _, err := PHCDecode(good); err != nil {
		t.Errorf("PHCDecode(%q) at floor should succeed, got %v", good, err)
	}
}

func TestPHC_KnownVector(t *testing.T) {
	s := "$argon2id$v=19$m=65536,t=3,p=1$c2FsdHNhbHRzYWx0c2FsdA$dGFndGFndGFndGFndGFndGFndGFndGFndA"
	got, err := PHCDecode(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Params.MemoryKiB != 65536 || got.Params.Iterations != 3 || got.Params.Parallelism != 1 {
		t.Errorf("params: %+v", got.Params)
	}
}
