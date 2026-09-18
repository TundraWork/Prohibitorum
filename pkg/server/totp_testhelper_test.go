// Package server — totp_testhelper_test.go
//
// Test-only TxRunner used by server unit tests that construct a totp.Store
// without spinning up a real *pgxpool.Pool. It runs the callback against the
// supplied TOTPQueries directly — no snapshot/rollback semantics — because
// these tests exercise the handler surface, not the rollback-on-failure
// path. The dedicated rollback test lives in pkg/credential/totp/totp_test.go
// where the fake fully supports tx semantics.

package server

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"testing"

	"prohibitorum/pkg/credential/totp"
)

func browserTOTPSecret(t *testing.T) string {
	t.Helper()
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
}

type totpTestTxRunner struct {
	q totp.TOTPQueries
}

func (r *totpTestTxRunner) InTx(ctx context.Context, fn func(q totp.TOTPQueries) error) error {
	return fn(r.q)
}
