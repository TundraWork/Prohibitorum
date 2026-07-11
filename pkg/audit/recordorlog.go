package audit

import (
	"context"

	"github.com/sirupsen/logrus"
)

// RecordOrLog writes r via w and, on failure, emits a structured error log
// carrying the factor/event/actor context that identifies the audit event
// WITHOUT including Detail, which may contain secrets, and WITHOUT
// surfacing the raw writer error, whose message may itself embed secrets
// (e.g. a database DSN password in a connection error).
//
// This is the fallback for the best-effort audit model: semantic audit rows
// (credential_event) are written when possible, but a failed insert must
// never be silently swallowed. The structured log preserves enough context
// (Factor, Event, AccountID, CredentialRef) for SOC/reconstruction while
// deliberately excluding Detail and the raw writer error to avoid leaking
// credentials, tokens, or other sensitive values into the log stream.
//
// A nil writer is a safe no-op so call sites that previously guarded with
// `if w != nil` can drop the guard and still never panic.
func RecordOrLog(ctx context.Context, w Writer, r Record) {
	if w == nil {
		return
	}
	if err := w.Record(ctx, r); err != nil {
		fields := logrus.Fields{
			"factor": string(r.Factor),
			"event":  r.Event,
		}
		if r.AccountID != nil {
			fields["account_id"] = *r.AccountID
		} else {
			fields["account_id"] = nil
		}
		if r.CredentialRef != nil {
			fields["credential_ref"] = *r.CredentialRef
		}
		// Do not attach the raw writer error: its message may embed secrets
		// (e.g. a connection DSN with a password). Emit only a generic
		// failure marker alongside the safe structured fields.
		_ = err
		logrus.WithContext(ctx).WithFields(fields).
			Error("audit write failed: semantic row unavailable, see structured log")
	}
}
