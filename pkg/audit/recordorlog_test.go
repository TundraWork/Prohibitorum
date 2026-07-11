package audit

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

// captureLogrus redirects the global logrus output into a buffer for the
// duration of the test. Must NOT run in parallel with other logrus-emitting
// tests — it mutates the global logger output.
func captureLogrus(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	origOut := logrus.StandardLogger().Out
	logrus.SetOutput(buf)
	return buf, func() { logrus.SetOutput(origOut) }
}

// TestRecordOrLog_FailureEmitsSafeStructuredLog is a security regression: when
// the audit writer fails with an error whose message embeds a secret DSN
// password, the fallback log MUST NOT surface the raw writer error (which
// would leak the secret). It must still carry factor/event/account/credential
// context and a generic failure marker, and must not leak the Detail map.
func TestRecordOrLog_FailureEmitsSafeStructuredLog(t *testing.T) {
	buf, restore := captureLogrus(t)
	defer restore()

	const secretPassword = "hunter2"
	sentinel := errors.New("pq: connection refused: postgres://audit:" + secretPassword + "@db.internal:5432/audit")
	w := NewWriter(&captureQ{err: sentinel})
	acctID := int32(42)
	credRef := int64(7)

	RecordOrLog(context.Background(), w, Record{
		AccountID:     &acctID,
		Factor:        FactorPassword,
		Event:         EventUse,
		CredentialRef: &credRef,
		Detail:        map[string]any{"secret": "should_not_appear_in_log"},
	})

	out := buf.String()
	if !strings.Contains(out, string(FactorPassword)) {
		t.Errorf("log missing factor %q\ngot: %s", FactorPassword, out)
	}
	if !strings.Contains(out, EventUse) {
		t.Errorf("log missing event %q\ngot: %s", EventUse, out)
	}
	if !strings.Contains(out, "account_id=42") {
		t.Errorf("log missing account_id=42\ngot: %s", out)
	}
	if !strings.Contains(out, "credential_ref=7") {
		t.Errorf("log missing credential_ref=7\ngot: %s", out)
	}
	if !strings.Contains(out, "audit write failed") {
		t.Errorf("log missing generic failure marker\ngot: %s", out)
	}
	if strings.Contains(out, secretPassword) {
		t.Errorf("log leaked raw writer error containing secret DSN password\ngot: %s", out)
	}
	if strings.Contains(out, "should_not_appear_in_log") {
		t.Errorf("log leaked Detail secret\ngot: %s", out)
	}
}

// TestRecordOrLog_SuccessNoLog proves that a successful audit insert does
// not emit any log — the database row is the record.
func TestRecordOrLog_SuccessNoLog(t *testing.T) {
	buf, restore := captureLogrus(t)
	defer restore()

	RecordOrLog(context.Background(), NewWriter(&captureQ{}), Record{
		Factor: FactorPassword,
		Event:  EventUse,
	})

	if buf.Len() > 0 {
		t.Errorf("expected no log on success, got: %s", buf.String())
	}
}

// TestRecordOrLog_NilWriterNoOp proves that a nil writer is a safe no-op,
// so call sites that previously guarded with `if s.Audit != nil` can drop
// the guard.
func TestRecordOrLog_NilWriterNoOp(t *testing.T) {
	buf, restore := captureLogrus(t)
	defer restore()

	RecordOrLog(context.Background(), nil, Record{
		Factor: FactorPassword,
		Event:  EventUse,
	})

	if buf.Len() > 0 {
		t.Errorf("expected no log for nil writer, got: %s", buf.String())
	}
}

// TestRecordOrLog_NilActorLogsCleanly proves that a nil AccountID (common
// for pre-auth failure events) produces a clean log without panicking. It
// must still carry the factor and a generic failure marker, but must not
// surface the raw writer error message.
func TestRecordOrLog_NilActorLogsCleanly(t *testing.T) {
	buf, restore := captureLogrus(t)
	defer restore()

	RecordOrLog(context.Background(), NewWriter(&captureQ{err: errors.New("boom with secret dsn pass=leaked")}), Record{
		AccountID: nil,
		Factor:    FactorWebAuthn,
		Event:     EventFail,
	})

	out := buf.String()
	if !strings.Contains(out, string(FactorWebAuthn)) {
		t.Errorf("log missing factor\ngot: %s", out)
	}
	if !strings.Contains(out, "audit write failed") {
		t.Errorf("log missing generic failure marker\ngot: %s", out)
	}
	if strings.Contains(out, "leaked") {
		t.Errorf("log leaked raw writer error\ngot: %s", out)
	}
}
