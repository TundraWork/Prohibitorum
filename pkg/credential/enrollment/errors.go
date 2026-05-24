package enrollment

import (
	"prohibitorum/pkg/authn"
)

func ErrEnrollmentExpired() *authn.AuthError {
	return authn.ErrEnrollmentExpired()
}

func ErrEnrollmentConsumed() *authn.AuthError {
	return authn.ErrEnrollmentConsumed()
}
