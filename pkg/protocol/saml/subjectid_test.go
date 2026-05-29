package saml

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/db"
)

// subjectIDKey identifies a stored NameID by its (account, sp) pair.
type subjectIDKey struct {
	accountID int32
	spID      int64
}

// fakeSubjectIDQueries is an in-memory db.Querier backing subjectID. Only
// GetSAMLSubjectID + InsertSAMLSubjectID are implemented; the embedded nil
// db.Querier satisfies the rest of the interface at compile time (any other
// method call would panic, which is what we want — the test must not reach
// them).
type fakeSubjectIDQueries struct {
	db.Querier
	store     map[subjectIDKey]db.SamlSubjectID
	getErr    error // forced error returned by GetSAMLSubjectID (overrides lookup)
	insertErr error // forced error returned by InsertSAMLSubjectID (overrides insert)
}

func newFakeSubjectIDQueries() *fakeSubjectIDQueries {
	return &fakeSubjectIDQueries{store: map[subjectIDKey]db.SamlSubjectID{}}
}

func (f *fakeSubjectIDQueries) GetSAMLSubjectID(_ context.Context, arg db.GetSAMLSubjectIDParams) (db.SamlSubjectID, error) {
	if f.getErr != nil {
		return db.SamlSubjectID{}, f.getErr
	}
	row, ok := f.store[subjectIDKey{arg.AccountID, arg.SpID}]
	if !ok {
		return db.SamlSubjectID{}, pgx.ErrNoRows
	}
	return row, nil
}

func (f *fakeSubjectIDQueries) InsertSAMLSubjectID(_ context.Context, arg db.InsertSAMLSubjectIDParams) (db.SamlSubjectID, error) {
	if f.insertErr != nil {
		return db.SamlSubjectID{}, f.insertErr
	}
	row := db.SamlSubjectID{
		AccountID:    arg.AccountID,
		SpID:         arg.SpID,
		NameID:       arg.NameID,
		NameIDFormat: arg.NameIDFormat,
	}
	f.store[subjectIDKey{arg.AccountID, arg.SpID}] = row
	return row, nil
}

const samlFormatPersistent = "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"

func TestSubjectIDGenerateAndPersist(t *testing.T) {
	fq := newFakeSubjectIDQueries()
	i := &IdP{queries: fq}
	ctx := context.Background()

	got, err := i.subjectID(ctx, 1, 100, samlFormatPersistent)
	if err != nil {
		t.Fatalf("subjectID: unexpected error: %v", err)
	}

	// 32 random bytes -> RawURLEncoding -> 43 chars, url-safe, no padding.
	if len(got) != 43 {
		t.Fatalf("subjectID length = %d, want 43 (base64url of 32 bytes); value=%q", len(got), got)
	}
	if strings.ContainsAny(got, "+/=") {
		t.Fatalf("subjectID %q contains non-url-safe / padding chars", got)
	}

	// The stored row must carry the format we passed in.
	stored, ok := fq.store[subjectIDKey{1, 100}]
	if !ok {
		t.Fatal("subjectID did not persist a row")
	}
	if stored.NameID != got {
		t.Fatalf("stored NameID %q != returned %q", stored.NameID, got)
	}
	if stored.NameIDFormat != samlFormatPersistent {
		t.Fatalf("stored NameIDFormat = %q, want %q", stored.NameIDFormat, samlFormatPersistent)
	}
}

func TestSubjectIDStable(t *testing.T) {
	fq := newFakeSubjectIDQueries()
	i := &IdP{queries: fq}
	ctx := context.Background()

	first, err := i.subjectID(ctx, 7, 200, samlFormatPersistent)
	if err != nil {
		t.Fatalf("subjectID (first): %v", err)
	}
	second, err := i.subjectID(ctx, 7, 200, samlFormatPersistent)
	if err != nil {
		t.Fatalf("subjectID (second): %v", err)
	}
	if first != second {
		t.Fatalf("subjectID not stable: first=%q second=%q", first, second)
	}
}

func TestSubjectIDDistinctPerSP(t *testing.T) {
	fq := newFakeSubjectIDQueries()
	i := &IdP{queries: fq}
	ctx := context.Background()

	idSP1, err := i.subjectID(ctx, 42, 1, samlFormatPersistent)
	if err != nil {
		t.Fatalf("subjectID sp1: %v", err)
	}
	idSP2, err := i.subjectID(ctx, 42, 2, samlFormatPersistent)
	if err != nil {
		t.Fatalf("subjectID sp2: %v", err)
	}
	if idSP1 == idSP2 {
		t.Fatalf("same NameID for two SPs (account 42): %q — must be unlinkable", idSP1)
	}
}

func TestSubjectIDGetErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom: db unavailable")
	fq := newFakeSubjectIDQueries()
	fq.getErr = sentinel
	i := &IdP{queries: fq}

	_, err := i.subjectID(context.Background(), 1, 1, samlFormatPersistent)
	if !errors.Is(err, sentinel) {
		t.Fatalf("subjectID error = %v, want %v (non-ErrNoRows must propagate)", err, sentinel)
	}
}

func TestSubjectIDInsertErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom: insert failed")
	fq := newFakeSubjectIDQueries()
	fq.insertErr = sentinel
	i := &IdP{queries: fq}

	_, err := i.subjectID(context.Background(), 1, 1, samlFormatPersistent)
	if !errors.Is(err, sentinel) {
		t.Fatalf("subjectID error = %v, want %v (insert error must propagate)", err, sentinel)
	}
}
