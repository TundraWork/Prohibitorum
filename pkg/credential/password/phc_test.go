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
