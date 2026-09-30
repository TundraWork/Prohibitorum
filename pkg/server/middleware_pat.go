package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/credential/pat"
	"prohibitorum/pkg/db"
)

// patAuthQueries is the DB surface patAuthMW needs.
type patAuthQueries interface {
	GetPATByTokenHash(ctx context.Context, tokenHash []byte) (db.PersonalAccessToken, error)
	GetAccountByID(ctx context.Context, id int32) (db.Account, error)
	TouchPATLastUsed(ctx context.Context, id int32) error
}

// patAuthMW authenticates management API requests that carry an
// X-Prohibitorum-PAT header (pat.SelectsManagementAuth). Without the header
// the request continues with its cookie session. Once the header is present
// (even empty) the request is judged as a PAT request alone: LoadSession has
// already left the cookie unread, and nothing here falls back to it.
//
// The principal goes on the context under its own key (authn.WithPATSession),
// so authn.SessionFromContext keeps returning cookie sessions only and public
// or protocol routes never see it. Protected operations reach it through
// registerOp and friends after authn.Check has cleared the route for PATs.
//
// It runs inside diagnosticCaptureMW so its refusals can be looked up by
// request ID, and it records the owner on the capture because the principal
// is not on the capture's own context.
func patAuthMW(q patAuthQueries, w audit.Writer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			if !pat.SelectsManagementAuth(r) {
				next.ServeHTTP(rw, r)
				return
			}
			ctx := r.Context()
			raw, err := pat.ParseHeader(r.Header[http.CanonicalHeaderKey(pat.HeaderName)])
			if err != nil {
				writeAuthErr(rw, authn.ErrPATInvalid())
				return
			}
			row, err := q.GetPATByTokenHash(ctx, pat.HashToken(raw))
			if errors.Is(err, pgx.ErrNoRows) {
				audit.RecordOrLog(ctx, w, audit.Record{
					Factor: audit.FactorPAT, Event: audit.EventFail,
					Detail: map[string]any{"reason": "pat_unknown"},
				})
				writeAuthErr(rw, authn.ErrPATInvalid())
				return
			}
			if err != nil {
				writeAuthErr(rw, fmt.Errorf("patAuthMW: token lookup: %w", err))
				return
			}
			ctx = audit.WithPATActor(ctx, row.ID)
			acctID := row.AccountID
			acct, err := q.GetAccountByID(ctx, row.AccountID)
			if errors.Is(err, pgx.ErrNoRows) {
				audit.RecordOrLog(ctx, w, audit.Record{
					AccountID: &acctID, Factor: audit.FactorPAT, Event: audit.EventFail,
					Detail: map[string]any{"reason": "account_missing"},
				})
				writeAuthErr(rw, authn.ErrPATInvalid())
				return
			}
			if err != nil {
				writeAuthErr(rw, fmt.Errorf("patAuthMW: owner lookup: %w", err))
				return
			}
			observeDiagnosticAccount(ctx, acct.ID)
			if acct.Disabled {
				audit.RecordOrLog(ctx, w, audit.Record{
					AccountID: &acctID, Factor: audit.FactorPAT, Event: audit.EventFail,
					Detail: map[string]any{"reason": "account_disabled"},
				})
				writeAuthErr(rw, authn.ErrAccountDisabled())
				return
			}
			access := pat.Access(row.Access)
			if !access.AllowsAPI() {
				audit.RecordOrLog(ctx, w, audit.Record{
					AccountID: &acctID, Factor: audit.FactorPAT, Event: audit.EventFail,
					Detail: map[string]any{"reason": "pat_api_not_allowed"},
				})
				writeAuthErr(rw, authn.ErrPATAPINotAllowed())
				return
			}
			ctx = authn.WithPATSession(ctx, &authn.Session{
				Account: &acct,
				PAT:     &authn.PATPrincipal{ID: row.ID, Access: access},
			})
			sw := &patStatusWriter{ResponseWriter: rw, status: http.StatusOK}
			next.ServeHTTP(sw, r.WithContext(ctx))
			// Only a request the route actually served counts as a use; one
			// refused for its route, role, sudo or maintenance does not.
			if sw.status < http.StatusBadRequest {
				_ = q.TouchPATLastUsed(context.WithoutCancel(ctx), row.ID)
			}
		})
	}
}

// patStatusWriter records the response status of a PAT-authenticated request.
// It forwards public-error observation, which weberr looks up by direct type
// assertion, so the diagnostic capture outside it still sees refusals.
type patStatusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *patStatusWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *patStatusWriter) Write(b []byte) (int, error) {
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}

func (w *patStatusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *patStatusWriter) ObservePublicError(code string, fields map[string]any) {
	if observer, ok := w.ResponseWriter.(interface {
		ObservePublicError(string, map[string]any)
	}); ok {
		observer.ObservePublicError(code, fields)
	}
}
