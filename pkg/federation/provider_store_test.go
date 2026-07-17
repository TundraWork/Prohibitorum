package federation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

type failingProviderQueries struct {
	err error
}

func (q failingProviderQueries) GetUpstreamIDPBySlugAny(context.Context, string) (db.UpstreamIdp, error) {
	return db.UpstreamIdp{}, q.err
}

func (failingProviderQueries) GetEnrollmentByToken(context.Context, string) (db.Enrollment, error) {
	return db.Enrollment{}, errors.New("unexpected enrollment lookup")
}

func TestProviderStoreBySlugClassifiesLookupFailureAsUnknown(t *testing.T) {
	lookupErr := errors.New("database unavailable")
	store := NewProviderStore(failingProviderQueries{err: lookupErr})

	_, err := store.BySlug(context.Background(), "corp")
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("BySlug error = %v, want opaque ErrUnknownProvider", err)
	}
	if errors.Is(err, lookupErr) {
		t.Fatalf("BySlug exposed underlying lookup failure: %v", err)
	}
}

func TestProviderFromRowMapsGenericConfigAndSecretHealth(t *testing.T) {
	t.Parallel()

	validatedAt := time.Date(2026, time.July, 16, 1, 2, 3, 0, time.UTC)
	row := db.UpstreamIdp{
		ID:                42,
		Slug:              "corp",
		DisplayName:       "Corporate",
		Protocol:          "oidc",
		Mode:              "invite_only",
		ProviderConfig:    []byte(`{"issuerUrl":"https://issuer.example"}`),
		SecretEnc:         []byte{1, 2, 3},
		SecretNonce:       []byte{4, 5, 6},
		KeyVersion:        pgtype.Int4{Int32: 7, Valid: true},
		SecretStatus:      "valid",
		SecretValidatedAt: pgtype.Timestamptz{Time: validatedAt, Valid: true},
		Disabled:          true,
	}

	provider, err := providerFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if string(provider.Config) != string(row.ProviderConfig) {
		t.Fatalf("Config = %s, want %s", provider.Config, row.ProviderConfig)
	}
	if provider.Secret == nil || provider.Secret.KeyVersion != 7 {
		t.Fatalf("Secret = %#v, want key version 7", provider.Secret)
	}
	if provider.SecretStatus != "valid" || provider.SecretValidatedAt == nil || !provider.SecretValidatedAt.Equal(validatedAt) {
		t.Fatalf("health = (%q, %v), want valid at %v", provider.SecretStatus, provider.SecretValidatedAt, validatedAt)
	}

	roundTrip, err := providerRow(provider)
	if err != nil {
		t.Fatal(err)
	}
	if string(roundTrip.ProviderConfig) != string(row.ProviderConfig) {
		t.Fatalf("round-trip config = %s, want %s", roundTrip.ProviderConfig, row.ProviderConfig)
	}
	if !roundTrip.KeyVersion.Valid || roundTrip.KeyVersion.Int32 != 7 {
		t.Fatalf("round-trip key version = %#v, want valid 7", roundTrip.KeyVersion)
	}
}

func TestProviderFromRowPreservesUnconfiguredNullSecretTuple(t *testing.T) {
	t.Parallel()

	provider, err := providerFromRow(db.UpstreamIdp{
		Slug:           "vrchat",
		Protocol:       "vrchat",
		ProviderConfig: []byte(`{}`),
		SecretStatus:   "unconfigured",
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Secret != nil {
		t.Fatalf("Secret = %#v, want nil", provider.Secret)
	}
	if provider.SecretStatus != "unconfigured" {
		t.Fatalf("SecretStatus = %q, want unconfigured", provider.SecretStatus)
	}

	row, err := providerRow(provider)
	if err != nil {
		t.Fatal(err)
	}
	if row.SecretEnc != nil || row.SecretNonce != nil || row.KeyVersion.Valid {
		t.Fatalf("row secret tuple = (%v, %v, %#v), want all null", row.SecretEnc, row.SecretNonce, row.KeyVersion)
	}
}
