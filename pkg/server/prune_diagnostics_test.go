// Package server — prune_diagnostics_test.go
//
// Lifecycle test for the expired-diagnostics reaper. The reaper runs once at
// startup then hourly; this test proves the startup prune fires and calls
// through to the diagnostic store's PruneExpired method.

package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/diagnostic"
)

// prunerDiagStore is a fake diagnostic store that counts PruneExpired calls.
// It implements diagnostic.StoreService (Lookup + PruneExpired).
type prunerDiagStore struct {
	diagnostic.StoreReader
	pruned int32
}

func (p *prunerDiagStore) Lookup(_ context.Context, _ string) (diagnostic.Record, error) {
	return diagnostic.Record{}, diagnostic.ErrNotFound
}

func (p *prunerDiagStore) PruneExpired(_ context.Context) error {
	atomic.AddInt32(&p.pruned, 1)
	return nil
}

func TestPruneExpiredDiagnostics_CalledAtStartup(t *testing.T) {
	store := &prunerDiagStore{}
	s := &Server{
		config:    &configx.Config{},
		diagStore: store,
	}

	// pruneExpiredDiagnosticsLoop prunes once at startup, then blocks on the
	// hourly ticker. Run it in a goroutine; check the startup prune fired.
	go s.pruneExpiredDiagnosticsLoop()

	// Wait for the startup prune to fire (best-effort polling).
	deadline := time.After(2 * time.Second)
	for {
		if atomic.LoadInt32(&store.pruned) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("PruneExpired was not called at startup")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestPruneExpiredDiagnostics_NilStoreNoOp(t *testing.T) {
	// A nil diagStore (e.g. NewHuma / openapi subcommand) must not panic.
	s := &Server{
		config:    &configx.Config{},
		diagStore: nil,
	}
	// The loop must guard against nil and return without panicking.
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Errorf("pruneExpiredDiagnosticsLoop panicked on nil store: %v", rec)
			}
		}()
		s.pruneExpiredDiagnosticsLoop()
	}()
	// Give the goroutine a moment to execute; if it panics, the deferred
	// recover above will report it. If it doesn't panic, it prunes once
	// (nil-safe) and blocks on the ticker — either way is success.
	time.Sleep(100 * time.Millisecond)
}
