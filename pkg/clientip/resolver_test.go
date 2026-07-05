package clientip

import (
	"context"
	"errors"
	"testing"
)

type fakeStore struct {
	get  Stored
	err  error
	sets []Stored
}

func (f *fakeStore) Get(context.Context) (Stored, error) { return f.get, f.err }
func (f *fakeStore) Set(_ context.Context, s Stored) error {
	f.sets = append(f.sets, s)
	return nil
}

func TestParseStored(t *testing.T) {
	ok := []Stored{
		{Strategy: "direct"},
		{Strategy: ""},
		{Strategy: "forwarded", TrustedProxies: []string{"203.0.113.0/24", "2001:db8::/32"}},
		{Strategy: "header", Header: "CF-Connecting-IP", TrustedProxies: []string{"10.0.0.0/8"}},
	}
	for _, s := range ok {
		if _, err := ParseStored(s); err != nil {
			t.Fatalf("ParseStored(%+v) unexpected error: %v", s, err)
		}
	}
	bad := []Stored{
		{Strategy: "bogus"},
		{Strategy: "header", Header: ""},
		{Strategy: "header", Header: "bad header!"},
		{Strategy: "forwarded", TrustedProxies: []string{"not-a-cidr"}},
		{Strategy: "forwarded", TrustedProxies: make([]string, maxTrustedProxies+1)},
	}
	for _, s := range bad {
		if _, err := ParseStored(s); err == nil {
			t.Fatalf("ParseStored(%+v) expected error, got nil", s)
		}
	}
}

func TestResolverCacheAndInvalidate(t *testing.T) {
	fs := &fakeStore{get: Stored{Strategy: "header", Header: "CF-Connecting-IP", TrustedProxies: []string{"203.0.113.0/24"}}}
	r := NewResolver(fs)
	if got := r.Config(context.Background()).Strategy; got != Header {
		t.Fatalf("strategy = %q, want header", got)
	}
	// Mutate the underlying store; cache must still return the old value.
	fs.get = Stored{Strategy: "direct"}
	if got := r.Config(context.Background()).Strategy; got != Header {
		t.Fatalf("cache broken: strategy = %q, want header", got)
	}
	r.Invalidate()
	if got := r.Config(context.Background()).Strategy; got != Direct {
		t.Fatalf("post-invalidate strategy = %q, want direct", got)
	}
}

func TestResolverReadErrorFailsSafe(t *testing.T) {
	r := NewResolver(&fakeStore{err: errors.New("db down")})
	if got := r.Config(context.Background()).Strategy; got != Direct {
		t.Fatalf("read-error strategy = %q, want direct", got)
	}
}

func TestResolverSetValidates(t *testing.T) {
	fs := &fakeStore{}
	r := NewResolver(fs)
	if err := r.Set(context.Background(), Stored{Strategy: "bogus"}); err == nil {
		t.Fatal("Set with bad strategy should error")
	}
	if len(fs.sets) != 0 {
		t.Fatal("invalid Set must not reach the store")
	}
	if err := r.Set(context.Background(), Stored{Strategy: "direct"}); err != nil {
		t.Fatalf("valid Set errored: %v", err)
	}
	if len(fs.sets) != 1 {
		t.Fatalf("valid Set stored %d times, want 1", len(fs.sets))
	}
}
