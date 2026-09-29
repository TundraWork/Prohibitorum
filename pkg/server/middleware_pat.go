package server

import (
	"context"
	"net/http"
	"strings"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/credential/pat"
	"prohibitorum/pkg/db"
)

const (
	managementAPIPrefix = "/api/prohibitorum/"
	// forwardAuthAPIPrefix is the gateway's own namespace: its verify endpoint
	// reads the same PAT header under the gateway's rules, so this middleware
	// must not answer for it.
	forwardAuthAPIPrefix = "/api/prohibitorum/forward-auth/"
)

// patAuthQueries is the DB surface patAuthMW needs.
type patAuthQueries interface {
	GetPATByTokenHash(ctx context.Context, tokenHash []byte) (db.PersonalAccessToken, error)
	GetAccountByID(ctx context.Context, id int32) (db.Account, error)
	TouchPATLastUsed(ctx context.Context, id int32) error
}

// patAuthMW authenticates management API requests that carry an
// X-Prohibitorum-PAT header. Without the header the request continues to the
// cookie session. Once the header is present (even empty) the request is judged
// as a PAT request alone and never falls back to a cookie.
//
// The principal goes on the context under its own key (authn.WithPATSession),
// so authn.SessionFromContext keeps returning cookie sessions only and public
// or protocol routes never see it. Protected operations reach it through
// registerOp and friends after authn.Check has cleared the route for PATs.
func patAuthMW(q patAuthQueries, w audit.Writer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, managementAPIPrefix) || strings.HasPrefix(r.URL.Path, forwardAuthAPIPrefix) {
				next.ServeHTTP(rw, r)
				return
			}
			values, present := r.Header[http.CanonicalHeaderKey(pat.HeaderName)]
			if !present {
				next.ServeHTTP(rw, r)
				return
			}
			ctx := r.Context()
			raw, err := pat.ParseHeader(values)
			if err != nil {
				writeAuthErr(rw, authn.ErrPATInvalid())
				return
			}
			row, err := q.GetPATByTokenHash(ctx, pat.HashToken(raw))
			if err != nil {
				audit.RecordOrLog(ctx, w, audit.Record{
					Factor: audit.FactorPAT, Event: audit.EventFail,
					Detail: map[string]any{"reason": "pat_unknown"},
				})
				writeAuthErr(rw, authn.ErrPATInvalid())
				return
			}
			acct, err := q.GetAccountByID(ctx, row.AccountID)
			if err != nil {
				writeAuthErr(rw, authn.ErrPATInvalid())
				return
			}
			if acct.Disabled {
				writeAuthErr(rw, authn.ErrAccountDisabled())
				return
			}
			access := pat.Access(row.Access)
			ctx = audit.WithPATActor(ctx, row.ID)
			if !access.AllowsAPI() {
				acctID := acct.ID
				audit.RecordOrLog(ctx, w, audit.Record{
					AccountID: &acctID, Factor: audit.FactorPAT, Event: audit.EventFail,
					Detail: map[string]any{"reason": "pat_api_not_allowed"},
				})
				writeAuthErr(rw, authn.ErrPATAPINotAllowed())
				return
			}
			_ = q.TouchPATLastUsed(ctx, row.ID)
			ctx = authn.WithPATSession(ctx, &authn.Session{
				Account: &acct,
				PAT:     &authn.PATPrincipal{ID: row.ID, Access: access},
			})
			next.ServeHTTP(rw, r.WithContext(ctx))
		})
	}
}
