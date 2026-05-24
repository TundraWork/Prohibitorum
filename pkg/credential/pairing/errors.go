package pairing

import (
	"prohibitorum/pkg/authn"
)

// Pairing errors — same not-found / state policy as enrollment: collapse
// "never existed", "expired", and "consumed" into one code so an attacker
// can't probe code validity.

func ErrPairingNotFound() *authn.AuthError {
	return authn.ErrPairingNotFound()
}

func ErrPairingState() *authn.AuthError {
	return authn.ErrPairingState()
}

func ErrPairingExpired() *authn.AuthError {
	return authn.ErrPairingExpired()
}

func ErrPairingNotApproved() *authn.AuthError {
	return authn.ErrPairingNotApproved()
}
