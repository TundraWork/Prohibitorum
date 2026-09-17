package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

const (
	PrincipalSourceSub           = "sub"
	PrincipalSourceUsername      = "username"
	PrincipalSourceVerifiedEmail = "verified_email"

	internalAccountIDClaim = "urn:prohibitorum:account_id"
)

var (
	errPrincipalUnavailable = errors.New("oidc: configured principal is unavailable")
	claimAliasNamePattern   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
)

var reservedClaimNames = map[string]struct{}{
	"iss": {}, "sub": {}, "aud": {}, "exp": {}, "iat": {},
	"auth_time": {}, "sid": {}, "amr": {}, "nonce": {},
	"acr": {}, "at_hash": {}, "azp": {}, internalAccountIDClaim: {},
}

var aliasSources = map[string]struct{}{
	"name": {}, "preferred_username": {}, "email": {}, "picture": {},
}

func validPrincipalSource(source string) bool {
	switch source {
	case PrincipalSourceSub, PrincipalSourceUsername, PrincipalSourceVerifiedEmail:
		return true
	default:
		return false
	}
}

func ValidPrincipalSource(source string) bool { return validPrincipalSource(source) }

func oidcPrincipalSource(source string) string {
	if source == "" {
		return PrincipalSourceSub
	}
	return source
}

func forwardAuthPrincipalSource(source string) string {
	if source == "" {
		return PrincipalSourceUsername
	}
	return source
}

func validateClaimAliases(aliases map[string]string) error {
	for name, source := range aliases {
		if !claimAliasNamePattern.MatchString(name) {
			return fmt.Errorf("invalid claim alias name %q", name)
		}
		if _, reserved := reservedClaimNames[name]; reserved {
			return fmt.Errorf("reserved claim alias name %q", name)
		}
		if _, ok := aliasSources[source]; !ok {
			return fmt.Errorf("invalid claim alias source %q", source)
		}
	}
	return nil
}

func ValidateClaimAliases(aliases map[string]string) error { return validateClaimAliases(aliases) }

func decodeClaimAliases(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}
	aliases := map[string]string{}
	if err := json.Unmarshal(raw, &aliases); err != nil {
		return nil, fmt.Errorf("decode claim aliases: %w", err)
	}
	if err := validateClaimAliases(aliases); err != nil {
		return nil, err
	}
	return aliases, nil
}

// resolvePrincipal computes the configured public identifier from current
// account data. Email comparison is case-insensitive, matching the way email
// ownership is checked elsewhere while preserving the stored spelling in the
// emitted value.
func resolvePrincipal(ctx context.Context, q db.Querier, account db.Account, source string) (string, error) {
	switch source {
	case PrincipalSourceSub:
		if subject := subjectOf(account); subject != "" {
			return subject, nil
		}
	case PrincipalSourceUsername:
		if username := strings.TrimSpace(account.Username); username != "" {
			return username, nil
		}
	case PrincipalSourceVerifiedEmail:
		if account.EmailVerified && account.Email.Valid && strings.TrimSpace(account.Email.String) != "" {
			count, err := q.CountVerifiedAccountsByEmail(ctx, account.Email.String)
			if err != nil {
				return "", err
			}
			if count == 1 {
				return account.Email.String, nil
			}
		}
	default:
		return "", fmt.Errorf("oidc: invalid principal source %q", source)
	}
	return "", errPrincipalUnavailable
}

func claimAliasSnapshot(account db.Account, scope []string, origin string) map[string]any {
	values := map[string]any{}
	if hasScope(scope, "profile") {
		profile := profileClaims(account, origin)
		for _, name := range []string{"name", "preferred_username", "picture"} {
			if value, ok := profile[name]; ok {
				values[name] = value
			}
		}
	}
	if hasScope(scope, "email") {
		if value, ok := emailClaims(account)["email"]; ok {
			values["email"] = value
		}
	}
	return values
}

func applyClaimAliases(claims map[string]any, account db.Account, scope []string, origin string, aliases map[string]string) {
	values := claimAliasSnapshot(account, scope, origin)
	for name, source := range aliases {
		if value, ok := values[source]; ok {
			claims[name] = value
		}
	}
}

func internalAccountID(claims map[string]any) (int32, bool) {
	value, ok := claims[internalAccountIDClaim]
	if !ok {
		return 0, false
	}
	switch id := value.(type) {
	case float64:
		if id > 0 && id <= float64(^uint32(0)>>1) && id == float64(int32(id)) {
			return int32(id), true
		}
	case int32:
		return id, id > 0
	case int:
		if id > 0 && int64(id) <= int64(^uint32(0)>>1) {
			return int32(id), true
		}
	}
	return 0, false
}

func accountFromTokenClaims(ctx context.Context, q db.Querier, claims map[string]any) (db.Account, error) {
	if accountID, ok := internalAccountID(claims); ok {
		return q.GetAccountByID(ctx, accountID)
	}
	sub, _ := claims["sub"].(string)
	var subject pgtype.UUID
	if sub == "" || subject.Scan(sub) != nil {
		return db.Account{}, errPrincipalUnavailable
	}
	return q.GetAccountByOIDCSubject(ctx, subject)
}
