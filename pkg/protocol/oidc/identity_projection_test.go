package oidc

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

type identityProjectionQueries struct {
	db.Querier
	verifiedEmailCount int64
}

func (q identityProjectionQueries) CountVerifiedAccountsByEmail(context.Context, string) (int64, error) {
	return q.verifiedEmailCount, nil
}

func projectionAccount(t *testing.T) db.Account {
	t.Helper()
	var subject pgtype.UUID
	if err := subject.Scan("11223344-5566-7788-99aa-bbccddeeff00"); err != nil {
		t.Fatal(err)
	}
	return db.Account{
		ID: 7, OidcSubject: subject, Username: "alice", DisplayName: "Alice",
		Email: pgtype.Text{String: "Alice@example.test", Valid: true}, EmailVerified: true,
	}
}

func TestResolvePrincipalSources(t *testing.T) {
	account := projectionAccount(t)
	tests := []struct {
		name, source, want string
		count              int64
	}{
		{name: "stable subject", source: PrincipalSourceSub, want: testSubject},
		{name: "username", source: PrincipalSourceUsername, want: "alice"},
		{name: "unique verified email", source: PrincipalSourceVerifiedEmail, want: "Alice@example.test", count: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolvePrincipal(context.Background(), identityProjectionQueries{verifiedEmailCount: tc.count}, account, tc.source)
			if err != nil || got != tc.want {
				t.Fatalf("resolvePrincipal() = %q, %v; want %q, nil", got, err, tc.want)
			}
		})
	}
}

func TestResolvePrincipalVerifiedEmailFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		verified bool
		count    int64
	}{
		{name: "unverified", count: 1},
		{name: "missing", verified: true},
		{name: "ambiguous", verified: true, count: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := projectionAccount(t)
			account.EmailVerified = tc.verified
			if tc.name == "missing" {
				account.Email = pgtype.Text{}
			}
			_, err := resolvePrincipal(context.Background(), identityProjectionQueries{verifiedEmailCount: tc.count}, account, PrincipalSourceVerifiedEmail)
			if !errors.Is(err, errPrincipalUnavailable) {
				t.Fatalf("error = %v, want principal unavailable", err)
			}
		})
	}
}

func TestApplyClaimAliasesUsesScopedPreOverwriteSnapshot(t *testing.T) {
	account := projectionAccount(t)
	claims := map[string]any{"name": "custom name", "preferred_username": "custom handle"}
	applyClaimAliases(claims, account, []string{"profile", "email"}, "https://issuer.example", map[string]string{
		"name":    "preferred_username",
		"handle":  "preferred_username",
		"contact": "email",
	})
	if claims["name"] != "alice" || claims["handle"] != "alice" || claims["contact"] != "Alice@example.test" {
		t.Fatalf("aliases = %#v", claims)
	}

	withoutScopes := map[string]any{}
	applyClaimAliases(withoutScopes, account, []string{"openid"}, "https://issuer.example", map[string]string{"handle": "preferred_username", "contact": "email"})
	if len(withoutScopes) != 0 {
		t.Fatalf("aliases escaped source scopes: %#v", withoutScopes)
	}
}

func TestValidateClaimAliases(t *testing.T) {
	if err := validateClaimAliases(map[string]string{"handle": "preferred_username", "login": "preferred_username"}); err != nil {
		t.Fatalf("duplicate sources must be allowed: %v", err)
	}
	for name, aliases := range map[string]map[string]string{
		"reserved":   {"sub": "name"},
		"bad name":   {"bad-name": "name"},
		"bad source": {"handle": "username"},
	} {
		t.Run(name, func(t *testing.T) {
			if validateClaimAliases(aliases) == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
