package totp

import (
	"context"
	"strings"
	"testing"
)

func TestGenerateRecoveryCode_Format(t *testing.T) {
	code, err := generateRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 19 {
		t.Errorf("len=%d, want 19", len(code))
	}
	if strings.Count(code, "-") != 3 {
		t.Errorf("hyphens=%d, want 3", strings.Count(code, "-"))
	}
	// Group spacing must land at positions 4, 9, 14.
	if code[4] != '-' || code[9] != '-' || code[14] != '-' {
		t.Errorf("hyphen positions wrong: %q", code)
	}
	for i, r := range strings.ReplaceAll(code, "-", "") {
		ok := (r >= 'A' && r <= 'Z') || (r >= '2' && r <= '7')
		if !ok {
			t.Errorf("char %d (%c) not RFC 4648 base32", i, r)
		}
	}
}

func TestGenerateRecoveryCode_Uniqueness(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 100; i++ {
		c, _ := generateRecoveryCode()
		if _, dup := seen[c]; dup {
			t.Fatal("duplicate code in 100 generations — randomness broken")
		}
		seen[c] = struct{}{}
	}
}

func TestNormalizeRecoveryCode(t *testing.T) {
	cases := map[string]string{
		"abcd-efgh-2345-6789":     "ABCDEFGH23456789",
		"ABCDEFGH23456789":        "ABCDEFGH23456789",
		" abcd-efgh-2345-6789 ":   "ABCDEFGH23456789",
		"abcdEFGH-2345-6789-aaaa": "ABCDEFGH23456789AAAA",
		"":                        "",
		"---":                     "",
	}
	for in, want := range cases {
		if got := normalizeRecoveryCode(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHashAndMatchRecoveryCodes(t *testing.T) {
	ctx := context.Background()
	codes := make([]string, 3)
	for i := range codes {
		code, err := generateRecoveryCode()
		if err != nil {
			t.Fatal(err)
		}
		codes[i] = normalizeRecoveryCode(code)
	}
	phcs, err := hashRecoveryCodes(ctx, codes)
	if err != nil {
		t.Fatalf("hashRecoveryCodes: %v", err)
	}
	if i, err := matchRecoveryCodes(ctx, codes[2], phcs); i != 2 || err != nil {
		t.Errorf("matchRecoveryCodes(codes[2]) = (%d, %v), want (2, nil)", i, err)
	}
	if i, err := matchRecoveryCodes(ctx, "WRONGCODE12345AB", phcs); i != -1 || err != nil {
		t.Errorf("matchRecoveryCodes(wrong) = (%d, %v), want (-1, nil)", i, err)
	}
}
