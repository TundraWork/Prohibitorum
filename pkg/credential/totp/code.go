// Package totp — code.go
//
// RFC 6238 TOTP code computation. Step = unix_seconds / period. HMAC-SHA1 is
// the ONLY supported algorithm; the function signature deliberately
// omits an algorithm parameter so callers cannot mistakenly assume dispatch.
// The implementation is inlined against the standard library — small enough
// that a third-party OTP dep is not worth the supply-chain surface. Widening
// to SHA-256 / SHA-512 is a future concern (once we control both authenticator
// and server endpoints); doing so will require adding an algorithm parameter
// here and updating all call sites in totp.go (Verify drift loop) and
// ComputeCodeForTesting.

package totp

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
)

func computeCode(secret []byte, step int64, digits int) string {
	counter := make([]byte, 8)
	binary.BigEndian.PutUint64(counter, uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(counter)
	digest := mac.Sum(nil)

	// RFC 4226 §5.3 dynamic truncation: low nibble of the last byte selects
	// the offset; the four bytes starting there yield a 31-bit unsigned int.
	offset := int(digest[len(digest)-1] & 0x0f)
	code := (int64(digest[offset]&0x7f)<<24 |
		int64(digest[offset+1]&0xff)<<16 |
		int64(digest[offset+2]&0xff)<<8 |
		int64(digest[offset+3]&0xff))

	mod := int64(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf(fmt.Sprintf("%%0%dd", digits), code%mod)
}

func stepFor(unixSeconds, period int64) int64 {
	return unixSeconds / period
}

// ComputeCodeForTesting exposes the HMAC-based code algorithm so external
// test tooling (cmd/smoke in Task 8) can drive a TOTP login flow against a
// running server. Production callers go through Store.Verify.
func ComputeCodeForTesting(secret []byte, unixSeconds int64, digits int) string {
	return computeCode(secret, stepFor(unixSeconds, 30), digits)
}
