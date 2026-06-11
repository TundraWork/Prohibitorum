package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

type Factor string

const (
	FactorWebAuthn       Factor = "webauthn"
	FactorPassword       Factor = "password"
	FactorTOTP           Factor = "totp"
	FactorRecoveryCode   Factor = "recovery_code"
	FactorFederationOIDC Factor = "federation_oidc"
	FactorEnrollment     Factor = "enrollment"
	FactorSession        Factor = "session"
	FactorOIDCClient     Factor = "oidc_client"
	FactorSAMLSP         Factor = "saml_sp"
	FactorUpstreamIDP    Factor = "upstream_idp"
	FactorSigningKey     Factor = "signing_key"
	// FactorAccount / FactorInvitation cover admin account-lifecycle and
	// invitation mutations so role escalations, disables, deletes, and invite
	// issue/revoke are visible in the audit viewer (not just the structured log).
	FactorAccount    Factor = "account"
	FactorInvitation Factor = "invitation"
)

const (
	EventRegister         = "register"
	EventUse              = "use"
	EventFail             = "fail"
	EventRevoke           = "revoke"
	EventCloneWarning     = "clone_warning"
	EventLink             = "link"
	EventUnlink           = "unlink"
	EventEnrollmentIssued   = "enrollment_issued"
	EventEnrollmentConsumed = "enrollment_consumed"
	EventSessionStart       = "session_start"
	EventSessionEnd         = "session_end"
	EventFactorDisabled     = "factor_disabled"
	// EventFactorLocked is emitted by the auth throttle on the transition
	// from "unlocked or expired lockout" → "now locked" so SOC pipelines
	// can detect lockouts without counting/aggregating fail rows.
	// OWASP MFA Cheat Sheet: log and alert on anomalies. The throttle
	// owns the transition signal, so it owns the audit emission.
	EventFactorLocked = "factor_locked"
	EventUpdate       = "update"
	EventRotate       = "rotate"
)

type Record struct {
	AccountID     *int32
	Factor        Factor
	Event         string
	CredentialRef *int64
	IP            *netip.Addr
	UserAgent     string
	Detail        map[string]any
}

type Writer interface {
	Record(ctx context.Context, r Record) error
}

func NewWriter(q db.Querier) Writer {
	return &dbWriter{q: q}
}

type dbWriter struct{ q db.Querier }

func (w *dbWriter) Record(ctx context.Context, r Record) error {
	var detail []byte
	if r.Detail != nil {
		b, err := json.Marshal(r.Detail)
		if err != nil {
			return fmt.Errorf("audit: marshal detail: %w", err)
		}
		detail = b
	}

	var credRef pgtype.Int8
	if r.CredentialRef != nil {
		credRef = pgtype.Int8{Int64: *r.CredentialRef, Valid: true}
	}

	var ua pgtype.Text
	if r.UserAgent != "" {
		ua = pgtype.Text{String: r.UserAgent, Valid: true}
	}

	if err := w.q.InsertCredentialEvent(ctx, db.InsertCredentialEventParams{
		AccountID:     r.AccountID,
		Factor:        string(r.Factor),
		Event:         r.Event,
		CredentialRef: credRef,
		Ip:            r.IP,
		UserAgent:     ua,
		Detail:        detail,
	}); err != nil {
		return fmt.Errorf("audit: insert credential_event: %w", err)
	}
	return nil
}

// ParseIPOrNil returns the parsed address or nil on any parse failure.
// Convention note: not "MustX" — never panics.
func ParseIPOrNil(s string) *netip.Addr {
	if s == "" {
		return nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		host, _, splitErr := net.SplitHostPort(s)
		if splitErr != nil {
			return nil
		}
		a, err = netip.ParseAddr(host)
		if err != nil {
			return nil
		}
	}
	return &a
}
