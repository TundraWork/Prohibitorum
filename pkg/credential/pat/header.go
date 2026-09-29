package pat

import (
	"errors"
	"strings"
	"unicode"
)

// HeaderName is the request header that carries a PAT, both at the
// forward-auth gateway and on the management API. The value is the raw token
// with no scheme; Authorization plays no part in PAT authentication.
const HeaderName = "X-Prohibitorum-PAT"

// ErrInvalidHeader means the header was present but did not hold exactly one
// well-formed token.
var ErrInvalidHeader = errors.New("pat: invalid header")

// ParseHeader extracts the raw token from the values of a present
// X-Prohibitorum-PAT header. It requires exactly one value that starts with
// Prefix and holds no whitespace or comma anywhere, so surrounding spaces are
// rejected rather than trimmed. Callers treat a missing header separately.
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
