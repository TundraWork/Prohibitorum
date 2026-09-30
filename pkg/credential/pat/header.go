package pat

import (
	"errors"
	"net/http"
	"strings"
	"unicode"
)

// HeaderName is the request header that carries a PAT, both at the
// forward-auth gateway and on the management API. The value is the raw token
// with no scheme; Authorization plays no part in PAT authentication.
const HeaderName = "X-Prohibitorum-PAT"

const (
	managementAPIPrefix = "/api/prohibitorum/"
	// forwardAuthAPIPrefix is the gateway's own namespace: its verify endpoint
	// reads the same header under the gateway's rules.
	forwardAuthAPIPrefix = "/api/prohibitorum/forward-auth/"
)

// ErrInvalidHeader means the header was present but did not hold exactly one
// well-formed token.
var ErrInvalidHeader = errors.New("pat: invalid header")

// SelectsManagementAuth reports whether r is a management API request that
// carries the PAT header, present even if empty. Such a request is
// authenticated by the token alone: the cookie session is not consulted.
func SelectsManagementAuth(r *http.Request) bool {
	p := r.URL.Path
	if !strings.HasPrefix(p, managementAPIPrefix) || strings.HasPrefix(p, forwardAuthAPIPrefix) {
		return false
	}
	_, present := r.Header[http.CanonicalHeaderKey(HeaderName)]
	return present
}

// ParseHeader extracts the raw token from the values of a present
// X-Prohibitorum-PAT header. It requires exactly one value that starts with
// Prefix and holds no whitespace or comma. Go's HTTP server already strips
// leading and trailing whitespace from header values before they get here, so
// in practice this rejects whitespace inside the value. Callers treat a
// missing header separately.
func ParseHeader(values []string) (string, error) {
	if len(values) != 1 {
		return "", ErrInvalidHeader
	}
	v := values[0]
	if !strings.HasPrefix(v, Prefix) || strings.ContainsRune(v, ',') {
		return "", ErrInvalidHeader
	}
	if strings.IndexFunc(v, unicode.IsSpace) >= 0 {
		return "", ErrInvalidHeader
	}
	return v, nil
}
