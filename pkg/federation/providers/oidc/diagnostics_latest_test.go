package oidc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"prohibitorum/pkg/kv"
)

// failingLatestStore rejects the TTL write the latest-outcome summary uses,
// while every other operation reaches the real store.
type failingLatestStore struct{ kv.Store }

func (failingLatestStore) SetEx(context.Context, string, string, time.Duration) error {
	return errors.New("store unavailable")
}

func TestLatestDiagnosticIsNilBeforeAnyRun(t *testing.T) {
	h := newDiagnosticHarness(t)
	latest, err := h.service.Latest(context.Background(), h.provider)
	if err != nil || latest != nil {
		t.Fatalf("latest=%+v err=%v", latest, err)
	}
}

func TestLatestDiagnosticSucceededKeepsSourcesAndPresenceNotValues(t *testing.T) {
	h := newDiagnosticHarness(t)
	h.run(t)
	latest, err := h.service.Latest(context.Background(), h.provider)
	if err != nil || latest == nil {
		t.Fatalf("latest=%+v err=%v", latest, err)
	}
	if latest.Status != "succeeded" || latest.Stale || latest.FinishedAt.IsZero() {
		t.Fatalf("latest=%+v", latest)
	}
	subject := latest.Fields["subject"]
	if subject.Source != "id_token" || subject.Claim != "sub" || !subject.Present {
		t.Fatalf("subject=%+v", subject)
	}
	if len(latest.Fields) != 7 {
		t.Fatalf("fields=%v", latest.Fields)
	}
	raw, err := h.service.store.Get(context.Background(), latestDiagnosticKey(h.provider.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"admin@example.com", "upstream-user", "Upstream Name", "eyJ"} {
		if strings.Contains(raw, value) {
			t.Fatalf("summary stores %q: %s", value, raw)
		}
	}
}

func TestLatestDiagnosticMarksAbsentClaims(t *testing.T) {
	h := newDiagnosticHarness(t)
	h.op.SetClaims("subject", "", true, "upstream-user", "")
	h.run(t)
	latest, _ := h.service.Latest(context.Background(), h.provider)
	if latest == nil || latest.Fields["email"].Present || latest.Fields["displayName"].Present || !latest.Fields["username"].Present {
		t.Fatalf("latest=%+v", latest)
	}
}

func TestLatestDiagnosticFailedRunIsRecorded(t *testing.T) {
	h := newDiagnosticHarness(t)
	h.tokenFailure.Store(true)
	h.run(t)
	latest, _ := h.service.Latest(context.Background(), h.provider)
	if latest == nil || latest.Status != "failed" || len(latest.Fields) != 0 {
		t.Fatalf("latest=%+v", latest)
	}
}

func TestLatestDiagnosticRecordsProviderRefusal(t *testing.T) {
	h := newDiagnosticHarness(t)
	s := h.start(t)
	if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, "", "access_denied", "", "", "", "", "callback"); err != nil {
		t.Fatal(err)
	}
	latest, _ := h.service.Latest(context.Background(), h.provider)
	if latest == nil || latest.Status != "failed" {
		t.Fatalf("latest=%+v", latest)
	}
}

func TestLatestDiagnosticRecordsConfigurationChangedFailure(t *testing.T) {
	h := newDiagnosticHarness(t)
	s := h.start(t)
	h.callback(t, s)
	h.configure(func(c *Config) { c.EmailClaim = "upn" })
	if _, err := h.service.Complete(context.Background(), s.ID, h.provider, h.browser, h.actor, "request"); err != nil {
		t.Fatal(err)
	}
	latest, _ := h.service.Latest(context.Background(), h.provider)
	if latest == nil || latest.Status != "failed" || !latest.Stale {
		t.Fatalf("latest=%+v", latest)
	}
}

func TestLatestDiagnosticIsStaleOnceConfigurationChanges(t *testing.T) {
	h := newDiagnosticHarness(t)
	h.run(t)
	h.configure(func(c *Config) { c.EmailClaim = "upn" })
	latest, _ := h.service.Latest(context.Background(), h.provider)
	if latest == nil || !latest.Stale || latest.Status != "succeeded" {
		t.Fatalf("latest=%+v", latest)
	}
}

func TestLatestDiagnosticIsPerProviderAndOverwritten(t *testing.T) {
	h := newDiagnosticHarness(t)
	h.run(t)
	other := h.provider
	other.ID++
	if latest, _ := h.service.Latest(context.Background(), other); latest != nil {
		t.Fatalf("another provider sees %+v", latest)
	}
	h.tokenFailure.Store(true)
	h.run(t)
	if latest, _ := h.service.Latest(context.Background(), h.provider); latest == nil || latest.Status != "failed" {
		t.Fatalf("latest=%+v", latest)
	}
}

func TestLatestDiagnosticWriteFailureLeavesTheRunIntact(t *testing.T) {
	h := newDiagnosticHarness(t)
	h.service.store = failingLatestStore{h.service.store}
	result := h.run(t)
	if result.Status != "succeeded" || result.Identity == nil {
		t.Fatalf("result=%+v", result)
	}
	if latest, err := h.service.Latest(context.Background(), h.provider); err != nil || latest != nil {
		t.Fatalf("latest=%+v err=%v", latest, err)
	}
}
