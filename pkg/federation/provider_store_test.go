package federation

import (
	"context"
	"errors"
	"testing"

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
