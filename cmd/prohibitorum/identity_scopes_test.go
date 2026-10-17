package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/db"
)

type cliProviders map[string]bool

func (p cliProviders) GetUpstreamIDPBySlugAny(_ context.Context, slug string) (db.UpstreamIdp, error) {
	if p == nil {
		return db.UpstreamIdp{}, errors.New("database unavailable")
	}
	if !p[slug] {
		return db.UpstreamIdp{}, pgx.ErrNoRows
	}
	return db.UpstreamIdp{Slug: slug}, nil
}

func TestCheckIdentityScopes(t *testing.T) {
	ctx := context.Background()
	providers := cliProviders{"vrchat": true}
	if err := checkIdentityScopes(ctx, providers, []string{"openid", "profile", "identity:vrchat"}); err != nil {
		t.Fatalf("known provider: %v", err)
	}
	if err := checkIdentityScopes(ctx, providers, []string{"openid", "identity:missing"}); err == nil || !strings.Contains(err.Error(), `"identity:missing" names no upstream identity provider`) {
		t.Fatalf("unknown provider: err = %v", err)
	}
	if err := checkIdentityScopes(ctx, cliProviders(nil), []string{"identity:vrchat"}); err == nil || !strings.Contains(err.Error(), "look up provider") {
		t.Fatalf("lookup failure: err = %v", err)
	}
}
