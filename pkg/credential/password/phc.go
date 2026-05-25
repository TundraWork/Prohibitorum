// Package password — phc.go
//
// PHC string encode/decode for argon2id per
// https://github.com/P-H-C/phc-string-format/blob/master/phc-sf-spec.md.
// Self-describing format lets us upgrade params over time.

package password

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"prohibitorum/pkg/configx"
)

var ErrPHCMalformed = errors.New("phc: malformed string")

type PHC struct {
	Params configx.PasswordHashParams
	Salt   []byte
	Tag    []byte
}

func PHCEncode(params configx.PasswordHashParams, salt, tag []byte) string {
	return fmt.Sprintf(
		"$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		params.MemoryKiB, params.Iterations, params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(tag),
	)
}

func PHCDecode(s string) (PHC, error) {
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[0] != "" {
		return PHC{}, ErrPHCMalformed
	}
	if parts[1] != "argon2id" {
		return PHC{}, fmt.Errorf("phc: unsupported variant %q", parts[1])
	}
	if parts[2] != "v=19" {
		return PHC{}, fmt.Errorf("phc: unsupported version %q", parts[2])
	}

	var p configx.PasswordHashParams
	for _, kv := range strings.Split(parts[3], ",") {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			return PHC{}, ErrPHCMalformed
		}
		key, val := kv[:eq], kv[eq+1:]
		n, err := strconv.ParseUint(val, 10, 32)
		if err != nil {
			return PHC{}, fmt.Errorf("phc: param %q: %w", key, err)
		}
		switch key {
		case "m":
			p.MemoryKiB = uint32(n)
		case "t":
			p.Iterations = uint32(n)
		case "p":
			p.Parallelism = uint8(n)
		default:
			return PHC{}, fmt.Errorf("phc: unknown param %q", key)
		}
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return PHC{}, fmt.Errorf("phc: salt: %w", err)
	}
	tag, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return PHC{}, fmt.Errorf("phc: tag: %w", err)
	}
	return PHC{Params: p, Salt: salt, Tag: tag}, nil
}
