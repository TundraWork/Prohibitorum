package totp

import "testing"

// TestRFC6238_AppendixB validates the HOTP/TOTP implementation against the
// canonical test vectors from RFC 6238 Appendix B. These are the de-facto
// conformance vectors; if any fail, our authenticator output won't match
// Google Authenticator / Authy / 1Password / etc.
//
// Note: RFC 6238 Appendix B publishes vectors for SHA-1 (20-byte key),
// SHA-256 (32-byte), and SHA-512 (64-byte). v0.2 only supports SHA-1, so we
// only assert that subset. The SHA-256 column for these timestamps uses a
// different 32-byte key per the RFC, not the SHA-1 key repeated.
func TestRFC6238_AppendixB(t *testing.T) {
	key := []byte("12345678901234567890")
	cases := []struct {
		time int64
		code string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
	}
	for _, c := range cases {
		step := c.time / 30
		got := computeCode(key, step, 8, "SHA1")
		if got != c.code {
			t.Errorf("T=%d step=%d: got %s want %s", c.time, step, got, c.code)
		}
	}
}

func TestStepFor(t *testing.T) {
	cases := []struct {
		unix, period, want int64
	}{
		{0, 30, 0},
		{29, 30, 0},
		{30, 30, 1},
		{59, 30, 1},
		{60, 30, 2},
		{1234567890, 30, 41152263},
	}
	for _, c := range cases {
		if got := stepFor(c.unix, c.period); got != c.want {
			t.Errorf("stepFor(%d, %d) = %d, want %d", c.unix, c.period, got, c.want)
		}
	}
}

func TestComputeCodeForTesting_RoundTrip(t *testing.T) {
	key := []byte("12345678901234567890")
	if got := ComputeCodeForTesting(key, 59, 8); got != "94287082" {
		t.Errorf("ComputeCodeForTesting(T=59) = %s, want 94287082", got)
	}
}
