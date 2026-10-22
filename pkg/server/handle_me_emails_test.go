package server

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/account/email"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/mail"
	"prohibitorum/pkg/weberr"
)

// recordingSender captures sent mail; optionally fails.
type recordingSender struct {
	mails []mail.Message
	err   error
	// failOnly marks a sender that is configured (Available) but whose
	// deliveries fail — the "SMTP configured but broken" case.
	failOnly bool
}

func (r *recordingSender) Send(_ context.Context, msg mail.Message) error {
	if r.err != nil {
		return r.err
	}
	r.mails = append(r.mails, msg)
	return nil
}
func (r *recordingSender) Available() bool { return r.err == nil || r.failOnly }

// recordingAuditWriter captures audit records.
type recordingAuditWriter struct{ records []audit.Record }

func (r *recordingAuditWriter) Record(_ context.Context, rec audit.Record) error {
	r.records = append(r.records, rec)
	return nil
}

func (r *recordingAuditWriter) find(factor audit.Factor, event string) *audit.Record {
	for i := range r.records {
		if r.records[i].Factor == factor && r.records[i].Event == event {
			return &r.records[i]
		}
	}
	return nil
}

// withEmailServerDB runs fn against a rolled-back transaction-backed server
// whose me-email transactions share the same pool tx.
func withEmailServerDB(t *testing.T, sender mail.Sender, fn func(ctx context.Context, s *Server, acctID int32)) {
	t.Helper()
	databaseURL := skipWithoutDB(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var acctID int32
	if err := tx.QueryRow(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle)
		VALUES ($1, $1, $2) RETURNING id`,
		"me_emails_"+time.Now().Format("150405.000000000"), []byte(time.Now().String())).Scan(&acctID); err != nil {
		t.Fatal(err)
	}

	limits, err := email.NewSendLimits(kv.NewMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	auditLog := &recordingAuditWriter{}
	s := &Server{
		queries:     db.New(tx),
		mailSender:  sender,
		emailLimits: limits,
		kvStore:     kv.NewMemoryStore(),
		Audit:       auditLog,
		config:      &configx.Config{Branding: configx.BrandingConfig{InstanceName: "Test"}},
	}
	// Route me-email transactions onto the same test transaction, so every
	// write lands inside the rollback scope.
	s.meEmailTxRunnerOverride = poolTxRunner{pool: pool, fallback: tx}
	fn(authn.WithSession(ctx, &authn.Session{
		Account: &db.Account{ID: acctID, Role: "user"},
		Data:    &authn.SessionData{},
	}), s, acctID)
	_ = auditLog // records are asserted via s.Audit if needed
}

// skipWithoutDB returns the test database URL, or skips the test when the
// environment names none — a machine without Postgres (the plain CI job)
// must not fall back to some local database.
func skipWithoutDB(t *testing.T) string {
	t.Helper()
	url := os.Getenv("PROHIBITORUM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PROHIBITORUM_TEST_DATABASE_URL is not set")
	}
	return url
}

func publicCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	pe := weberr.AsPublic(err)
	if pe == nil {
		t.Fatalf("error %v is not a public error", err)
	}
	return pe.Code
}

func TestMeEmailsFullFlow(t *testing.T) {
	sender := &recordingSender{}
	withEmailServerDB(t, sender, func(ctx context.Context, s *Server, acctID int32) {
		// Add.
		in := &addMyEmailIn{}
		in.Body.Address = "flow@example.test"
		added, err := s.handleAddMyEmail(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if added.Body.Verified || added.Body.Primary {
			t.Errorf("fresh address: verified=%v primary=%v", added.Body.Verified, added.Body.Primary)
		}

		// Send code.
		sendIn := &sendMyEmailCodeIn{ID: added.Body.ID}
		sendIn.Body.Locale = "zh"
		if _, err := s.handleSendMyEmailCode(ctx, sendIn); err != nil {
			t.Fatal(err)
		}
		if len(sender.mails) != 1 {
			t.Fatalf("sent %d mails, want 1", len(sender.mails))
		}
		if !strings.Contains(sender.mails[0].Text, "验证码") {
			t.Errorf("zh locale mail: %q", sender.mails[0].Text)
		}
		code := extractCode(t, sender.mails[0].Text)

		// Verify → becomes primary (first address on the account).
		verifyIn := &verifyMyEmailIn{ID: added.Body.ID}
		verifyIn.Body.Code = code
		verified, err := s.handleVerifyMyEmail(ctx, verifyIn)
		if err != nil {
			t.Fatal(err)
		}
		if !verified.Body.Verified || !verified.Body.Primary {
			t.Errorf("verified: %+v", verified.Body)
		}

		// Audit rows landed.
		rec := s.Audit.(*recordingAuditWriter)
		if rec.find(audit.FactorEmail, audit.EventRegister) == nil ||
			rec.find(audit.FactorEmail, audit.EventVerificationSent) == nil ||
			rec.find(audit.FactorEmail, audit.EventVerified) == nil ||
			rec.find(audit.FactorEmail, audit.EventPrimaryChanged) == nil {
			t.Errorf("missing audit rows: %+v", rec.records)
		}
	})
}

func extractCode(t *testing.T, body string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 6 && allDigits(line) {
			return line
		}
	}
	// zh template prefixes 验证码：; en "Verification code:"
	for _, line := range strings.Split(body, "\n") {
		if idx := strings.LastIndex(line, "："); idx >= 0 {
			if code := line[idx+len("："):]; len(code) == 6 {
				return code
			}
		}
		if idx := strings.LastIndex(line, ": "); idx >= 0 {
			if code := line[idx+len(": "):]; len(code) == 6 {
				return code
			}
		}
	}
	t.Fatalf("no code found in %q", body)
	return ""
}

func TestMeEmailsSendCodeWithoutSMTP(t *testing.T) {
	withEmailServerDB(t, mail.NullSender{}, func(ctx context.Context, s *Server, acctID int32) {
		added := mustAdd(t, ctx, s, "nosmtp@example.test")
		sendIn := &sendMyEmailCodeIn{ID: added}
		if _, err := s.handleSendMyEmailCode(ctx, sendIn); publicCode(t, err) != "email_delivery_unavailable" {
			t.Fatal("want email_delivery_unavailable")
		}
	})
}

func TestMeEmailsCooldownReturns429WithRetryAfter(t *testing.T) {
	sender := &recordingSender{}
	withEmailServerDB(t, sender, func(ctx context.Context, s *Server, acctID int32) {
		added := mustAdd(t, ctx, s, "cooldown@example.test")
		sendIn := &sendMyEmailCodeIn{ID: added}
		if _, err := s.handleSendMyEmailCode(ctx, sendIn); err != nil {
			t.Fatal(err)
		}
		_, err := s.handleSendMyEmailCode(ctx, sendIn)
		if publicCode(t, err) != "rate_limited" {
			t.Fatal("want rate_limited on cooldown resend")
		}
		pe := weberr.AsPublic(err)
		if pe == nil || pe.RetryAfterSeconds <= 0 {
			t.Fatalf("RetryAfterSeconds = %+v", pe)
		}
	})
}

func TestMeEmailsSendFailureDropsCode(t *testing.T) {
	sender := &recordingSender{err: errors.New("smtp down"), failOnly: true}
	withEmailServerDB(t, sender, func(ctx context.Context, s *Server, acctID int32) {
		added := mustAdd(t, ctx, s, "dropped@example.test")
		sendIn := &sendMyEmailCodeIn{ID: added}
		if _, err := s.handleSendMyEmailCode(ctx, sendIn); publicCode(t, err) != "email_delivery_failed" {
			t.Fatal("want email_delivery_failed")
		}
		// The code must be gone: a correct-length guess can't succeed.
		verifyIn := &verifyMyEmailIn{ID: added}
		verifyIn.Body.Code = "000000"
		if _, err := s.handleVerifyMyEmail(ctx, verifyIn); publicCode(t, err) != "email_code_expired" {
			t.Fatal("want email_code_expired after the code was dropped")
		}
	})
}

func TestMeEmailsForeignVerifiedAddressIsUnavailable(t *testing.T) {
	databaseURL := skipWithoutDB(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := db.New(tx)

	var owner int32
	if err := tx.QueryRow(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle)
		VALUES ($1, $1, $2) RETURNING id`,
		"owner_"+time.Now().Format("150405.000000000"), []byte(time.Now().String()+"o")).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	var idpID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO upstream_idp (slug, display_name, protocol, mode, provider_config, secret_status, secret_enc, secret_nonce, key_version)
		VALUES ($1, 'Taken IdP', 'oidc', 'auto_provision', '{}'::jsonb, 'unconfigured', NULL, NULL, NULL)
		RETURNING id`, "taken_"+time.Now().Format("150405.000000000")).Scan(&idpID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := email.AbsorbUpstream(ctx, q, owner, "taken@example.test", idpID); err != nil {
		t.Fatal(err)
	}

	limits, _ := email.NewSendLimits(kv.NewMemoryStore())
	s := &Server{
		queries:     q,
		mailSender:  &recordingSender{},
		emailLimits: limits,
		kvStore:     kv.NewMemoryStore(),
		Audit:       &recordingAuditWriter{},
		config:      &configx.Config{},
	}
	s.meEmailTxRunnerOverride = poolTxRunner{pool: pool, fallback: tx}
	var challenger int32
	if err := tx.QueryRow(ctx, `
		INSERT INTO account (username, display_name, webauthn_user_handle)
		VALUES ($1, $1, $2) RETURNING id`,
		"challenger_"+time.Now().Format("150405.000000000"), []byte(time.Now().String()+"c")).Scan(&challenger); err != nil {
		t.Fatal(err)
	}
	cctx := authn.WithSession(ctx, &authn.Session{Account: &db.Account{ID: challenger}, Data: &authn.SessionData{}})

	in := &addMyEmailIn{}
	in.Body.Address = "taken@example.test"
	added, err := s.handleAddMyEmail(cctx, in)
	if err != nil {
		t.Fatal(err)
	}
	// Verify against a code: issue directly so no mail is needed.
	code, err := email.Issue(cctx, s.kvStore, added.Body.ID, challenger, "taken@example.test")
	if err != nil {
		t.Fatal(err)
	}
	verifyIn := &verifyMyEmailIn{ID: added.Body.ID}
	verifyIn.Body.Code = code
	_, verr := s.handleVerifyMyEmail(cctx, verifyIn)
	if publicCode(t, verr) != "email_unavailable" {
		t.Fatalf("want email_unavailable, got %v", verr)
	}
	pe := weberr.AsPublic(verr)
	if pe.Details != nil && len(pe.Details) > 0 {
		t.Errorf("email_unavailable leaked details: %v", pe.Details)
	}
}

func TestMeEmailsForeignIDIs404(t *testing.T) {
	sender := &recordingSender{}
	withEmailServerDB(t, sender, func(ctx context.Context, s *Server, acctID int32) {
		// Add on the session's account, then act as another account.
		added := mustAdd(t, ctx, s, "mine@example.test")
		otherCtx := authn.WithSession(context.Background(), &authn.Session{
			Account: &db.Account{ID: acctID + 999999}, Data: &authn.SessionData{},
		})
		sendIn := &sendMyEmailCodeIn{ID: added}
		if _, sendErr := s.handleSendMyEmailCode(otherCtx, sendIn); sendErr == nil {
			t.Fatal("foreign send-code accepted")
		} else if code := publicCode(t, sendErr); code != "not_found" && code != "email_not_found" {
			t.Fatalf("foreign send-code code = %s", code)
		}
		delIn := &deleteMyEmailIn{ID: added}
		if _, delErr := s.handleDeleteMyEmail(otherCtx, delIn); delErr == nil {
			t.Fatal("foreign delete accepted")
		} else if code := publicCode(t, delErr); code != "not_found" && code != "email_not_found" {
			t.Fatalf("foreign delete code = %s", code)
		}
	})
}

func TestMeEmailsPrimaryChangeAndDeleteWithoutSudo(t *testing.T) {
	sender := &recordingSender{}
	withEmailServerDB(t, sender, func(ctx context.Context, s *Server, acctID int32) {
		// Session has no SudoUntil: all operations must still succeed.
		first := mustAdd(t, ctx, s, "first@example.test")
		sendAndVerify(t, ctx, s, sender, first)
		second := mustAdd(t, ctx, s, "second@example.test")
		sendAndVerify(t, ctx, s, sender, second)

		// Second address verified but not primary: make it primary.
		setIn := &setMyEmailPrimaryIn{ID: second}
		_, err := s.handleSetMyEmailPrimary(ctx, setIn)
		if err != nil {
			t.Fatalf("set primary without sudo: %v", err)
		}

		// The old primary can now be deleted.
		delIn := &deleteMyEmailIn{ID: first}
		if _, err := s.handleDeleteMyEmail(ctx, delIn); err != nil {
			t.Fatalf("delete without sudo: %v", err)
		}
		// The (current) primary cannot.
		delIn = &deleteMyEmailIn{ID: second}
		if _, delErr := s.handleDeleteMyEmail(ctx, delIn); delErr == nil {
			t.Fatal("primary delete accepted")
		} else if code := publicCode(t, delErr); code != "email_primary_undeletable" {
			t.Fatalf("delete primary code = %s", code)
		}
	})
}

func mustAdd(t *testing.T, ctx context.Context, s *Server, address string) int64 {
	t.Helper()
	in := &addMyEmailIn{}
	in.Body.Address = address
	out, err := s.handleAddMyEmail(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	return out.Body.ID
}

func sendAndVerify(t *testing.T, ctx context.Context, s *Server, sender *recordingSender, emailID int64) {
	t.Helper()
	sendIn := &sendMyEmailCodeIn{ID: emailID}
	if _, err := s.handleSendMyEmailCode(ctx, sendIn); err != nil {
		t.Fatal(err)
	}
	verifyIn := &verifyMyEmailIn{ID: emailID}
	verifyIn.Body.Code = extractCode(t, sender.mails[len(sender.mails)-1].Text)
	if _, err := s.handleVerifyMyEmail(ctx, verifyIn); err != nil {
		t.Fatal(err)
	}
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// poolTxRunner opens me-email transactions as savepoints on an outer test
// transaction, so writes are visible to s.queries and rolled back together.
type poolTxRunner struct {
	pool     *pgxpool.Pool
	fallback pgx.Tx
}

func (r poolTxRunner) BeginMeEmailTx(ctx context.Context) (meEmailTx, error) {
	// Savepoint on the outer test transaction, so the handler's writes see
	// its uncommitted rows and share its rollback.
	tx, err := r.fallback.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &poolTx{tx: tx}, nil
}

type poolTx struct{ tx pgx.Tx }

func (t *poolTx) Queries() email.Queries {
	// Delegate to the outer transaction's queries so everything shares one
	// rollback scope; the fresh tx exists only to satisfy the interface.
	return poolTxQueries{tx: t.tx}
}
func (t *poolTx) Commit(ctx context.Context) error   { return t.tx.Commit(ctx) }
func (t *poolTx) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }

// poolTxQueries adapts a pgx.Tx into the email write surface.
type poolTxQueries struct{ tx pgx.Tx }

func (p poolTxQueries) GetAccountByIDForUpdate(ctx context.Context, id int32) (db.Account, error) {
	return db.New(p.tx).GetAccountByIDForUpdate(ctx, id)
}
func (p poolTxQueries) CountAccountEmails(ctx context.Context, accountID int32) (int64, error) {
	return db.New(p.tx).CountAccountEmails(ctx, accountID)
}
func (p poolTxQueries) GetAccountEmailByAddress(ctx context.Context, arg db.GetAccountEmailByAddressParams) (db.GetAccountEmailByAddressRow, error) {
	return db.New(p.tx).GetAccountEmailByAddress(ctx, arg)
}
func (p poolTxQueries) InsertAccountEmail(ctx context.Context, arg db.InsertAccountEmailParams) (db.AccountEmail, error) {
	return db.New(p.tx).InsertAccountEmail(ctx, arg)
}
func (p poolTxQueries) InsertAccountEmailVerified(ctx context.Context, arg db.InsertAccountEmailVerifiedParams) (db.AccountEmail, error) {
	return db.New(p.tx).InsertAccountEmailVerified(ctx, arg)
}
func (p poolTxQueries) MarkAccountEmailVerified(ctx context.Context, arg db.MarkAccountEmailVerifiedParams) (int64, error) {
	return db.New(p.tx).MarkAccountEmailVerified(ctx, arg)
}
func (p poolTxQueries) DeleteAccountEmail(ctx context.Context, arg db.DeleteAccountEmailParams) (int64, error) {
	return db.New(p.tx).DeleteAccountEmail(ctx, arg)
}
func (p poolTxQueries) GetAccountEmail(ctx context.Context, arg db.GetAccountEmailParams) (db.GetAccountEmailRow, error) {
	return db.New(p.tx).GetAccountEmail(ctx, arg)
}
func (p poolTxQueries) SetAccountPrimaryEmail(ctx context.Context, arg db.SetAccountPrimaryEmailParams) error {
	return db.New(p.tx).SetAccountPrimaryEmail(ctx, arg)
}
func (p poolTxQueries) ClearAccountPrimaryEmail(ctx context.Context, id int32) error {
	return db.New(p.tx).ClearAccountPrimaryEmail(ctx, id)
}
func (p poolTxQueries) PickFallbackPrimary(ctx context.Context, arg db.PickFallbackPrimaryParams) (db.PickFallbackPrimaryRow, error) {
	return db.New(p.tx).PickFallbackPrimary(ctx, arg)
}
func (p poolTxQueries) CountVerifiedAccountEmailsByAddress(ctx context.Context, arg db.CountVerifiedAccountEmailsByAddressParams) (int64, error) {
	return db.New(p.tx).CountVerifiedAccountEmailsByAddress(ctx, arg)
}
func (p poolTxQueries) GetAccountPrimaryEmail(ctx context.Context, id int32) (string, error) {
	return db.New(p.tx).GetAccountPrimaryEmail(ctx, id)
}
