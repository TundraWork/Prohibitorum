package oidc

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

type identityProjectionQueries struct {
	db.Querier
	primary    string
	primaryErr error
}

func (q identityProjectionQueries) GetAccountPrimaryEmail(context.Context, int32) (string, error) {
	if q.primaryErr != nil {
		return "", q.primaryErr
	}
	return q.primary, nil
}

func projectionAccount(t *testing.T) db.Account {
	t.Helper()
	var subject pgtype.UUID
	if err := subject.Scan("11223344-5566-7788-99aa-bbccddeeff00"); err != nil {
		t.Fatal(err)
	}
	return db.Account{
		ID: 7, OidcSubject: subject, Username: "alice", DisplayName: "Alice",
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
		{name: "primary email", source: PrincipalSourceVerifiedEmail, want: "Alice@example.test"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolvePrincipal(context.Background(), identityProjectionQueries{primary: "Alice@example.test"}, account, tc.source)
			if err != nil || got != tc.want {
				t.Fatalf("resolvePrincipal() = %q, %v; want %q, nil", got, err, tc.want)
			}
		})
	}
}

func TestResolvePrincipalVerifiedEmailFailsClosed(t *testing.T) {
	// No primary (ErrNoRows from the query) → unavailable; blank primary →
	// unavailable.
	for _, tc := range []struct {
		name    string
		queries identityProjectionQueries
	}{
		{name: "no primary row", queries: identityProjectionQueries{primaryErr: pgx.ErrNoRows}},
		{name: "blank primary", queries: identityProjectionQueries{primary: "   "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := projectionAccount(t)
			_, err := resolvePrincipal(context.Background(), tc.queries, account, PrincipalSourceVerifiedEmail)
			if !errors.Is(err, errPrincipalUnavailable) {
				t.Fatalf("error = %v, want principal unavailable", err)
			}
		})
	}
}

func TestApplyClaimAliasesUsesScopedPreOverwriteSnapshot(t *testing.T) {
	account := projectionAccount(t)
	claims := map[string]any{"name": "custom name", "preferred_username": "custom handle"}
	applyClaimAliases(claims, account, "Alice@example.test", []string{"profile", "email"}, "https://issuer.example", map[string]string{
		"name":    "preferred_username",
		"handle":  "preferred_username",
		"contact": "email",
	})
	if claims["name"] != "alice" || claims["handle"] != "alice" || claims["contact"] != "Alice@example.test" {
		t.Fatalf("aliases = %#v", claims)
	}

	withoutScopes := map[string]any{}
	applyClaimAliases(withoutScopes, account, "Alice@example.test", []string{"openid"}, "https://issuer.example", map[string]string{"handle": "preferred_username", "contact": "email"})
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
