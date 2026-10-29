// Package totp — recovery.go
//
// Recovery codes: 16 base32 characters (RFC 4648 alphabet, no padding)
// formatted as XXXX-XXXX-XXXX-XXXX. 80 bits of entropy per code, single-use,
// argon2id-hashed via password.HashRawEach / password.VerifyRawAny (Task 2).
// Reuse the password store's argon2id wiring rather than duplicating it — the
// recovery code threat model is identical (verify a low-entropy secret in
// constant time against an at-rest hash). A whole batch takes one argon2id
// slot, so a busy server delays minting or matching once, not once per code.

package totp

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"

	"prohibitorum/pkg/credential/password"
)

const (
	// RFC 4648 base32 uppercase alphabet (A-Z plus 2-7), 32 symbols.
	recoveryCodeChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	// 16 base32 chars × 5 bits = 80 bits entropy.
	recoveryCodeLen = 16
)

func generateRecoveryCode() (string, error) {
	raw := make([]byte, recoveryCodeLen)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("totp: recovery rand: %w", err)
	}
	chars := make([]byte, recoveryCodeLen)
	// 256 mod 32 == 0, so the uniform 8-bit source maps to the 32-symbol
	// alphabet with zero modulo bias.
	for i, b := range raw {
		chars[i] = recoveryCodeChars[int(b)%len(recoveryCodeChars)]
	}
	return fmt.Sprintf("%s-%s-%s-%s",
		chars[0:4], chars[4:8], chars[8:12], chars[12:16]), nil
}

func normalizeRecoveryCode(input string) string {
	s := strings.TrimSpace(input)
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ToUpper(s)
	return s
}

// hashRecoveryCodes and matchRecoveryCodes are the batch argon2id seams. They
// are overridden only by non-parallel tests; do not add t.Parallel to tests
// that swap them.
var (
	hashRecoveryCodes = func(ctx context.Context, normalized []string) ([]string, error) {
		return password.HashRawEach(ctx, normalized, password.DefaultParams())
	}
	matchRecoveryCodes = password.VerifyRawAny
)
