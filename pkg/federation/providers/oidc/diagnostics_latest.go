package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	federationcore "prohibitorum/pkg/federation"
	"prohibitorum/pkg/kv"
)

// latestDiagnosticTTL keeps a provider's last connection-test outcome long
// enough to still be useful when an administrator comes back to the page.
const latestDiagnosticTTL = 30 * 24 * time.Hour

// LatestDiagnosticField says where one identity field was read and whether the
// provider returned it, never the value: every administrator can read the
// summary, but the tested account is only the one who ran the test.
type LatestDiagnosticField struct {
	Source  string `json:"source"`
	Claim   string `json:"claim,omitempty"`
	Present bool   `json:"present"`
}

// LatestDiagnostic is the outcome of a provider's most recent finished test.
// Stale is set on read, when the provider's configuration has changed since.
type LatestDiagnostic struct {
	Status     string                           `json:"status"`
	FinishedAt time.Time                        `json:"finishedAt"`
	Stale      bool                             `json:"stale"`
	Fields     map[string]LatestDiagnosticField `json:"fields"`
}

type storedLatestDiagnostic struct {
	Status           string                           `json:"status"`
	FinishedAt       time.Time                        `json:"finishedAt"`
	ProviderIdentity string                           `json:"providerIdentity"`
	Fields           map[string]LatestDiagnosticField `json:"fields"`
}

func latestDiagnosticKey(providerID int64) string {
	return "oidc:admin-test-latest:" + strconv.FormatInt(providerID, 10)
}

// recordLatest remembers a finished run. A failed write is logged and dropped:
// the run's own result must never depend on the summary.
func (d *Diagnostics) recordLatest(ctx context.Context, flow diagnosticFlow) {
	summary := storedLatestDiagnostic{Status: flow.Status, FinishedAt: d.now().UTC(), ProviderIdentity: flow.ProviderIdentity, Fields: map[string]LatestDiagnosticField{}}
	if identity := flow.DiagnosticResult.Identity; identity != nil {
		for name, field := range map[string]DiagnosticField{"issuer": identity.Issuer, "subject": identity.Subject, "username": identity.Username, "displayName": identity.DisplayName, "email": identity.Email, "emailVerified": identity.EmailVerified, "picture": identity.Picture} {
			summary.Fields[name] = LatestDiagnosticField{Source: field.Source, Claim: field.Claim, Present: field.Value != nil}
		}
	}
	raw, err := json.Marshal(summary)
	if err == nil {
		err = d.store.SetEx(context.WithoutCancel(ctx), latestDiagnosticKey(flow.ProviderID), string(raw), latestDiagnosticTTL)
	}
	if err != nil {
		slog.WarnContext(ctx, "oidc: record latest connection test failed", "provider_id", flow.ProviderID, "error", err)
	}
}

// ForgetLatest drops a provider's remembered test outcome. It is for a
// provider that no longer exists, so the summary does not sit unread until its
// TTL runs out. Best effort: a failed delete is logged and the caller carries
// on, because the outcome is only a convenience.
func (d *Diagnostics) ForgetLatest(ctx context.Context, providerID int64) {
	if err := d.store.Del(context.WithoutCancel(ctx), latestDiagnosticKey(providerID)); err != nil {
		slog.WarnContext(ctx, "oidc: forget latest connection test failed", "provider_id", providerID, "error", err)
	}
}

// Latest returns the provider's most recent finished test, or nil when none
// is remembered.
func (d *Diagnostics) Latest(ctx context.Context, provider federationcore.Provider) (*LatestDiagnostic, error) {
	raw, err := d.store.Get(ctx, latestDiagnosticKey(provider.ID))
	if errors.Is(err, kv.ErrKeyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var stored storedLatestDiagnostic
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, err
	}
	return &LatestDiagnostic{Status: stored.Status, FinishedAt: stored.FinishedAt, Stale: stored.ProviderIdentity != providerIdentity(provider), Fields: stored.Fields}, nil
}
