package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
	sessstore "prohibitorum/pkg/session"
)

// accountChooserContinuePath is the address "use another account" signs in
// and returns to; the sign-in that lands there binds its account to the
// ticket (bindChooserTicket).
const accountChooserContinuePath = "/api/prohibitorum/account-chooser/continue"

// accountChooserPagePath is the dashboard's chooser page.
const accountChooserPagePath = "/choose-account"

// appAccessEvaluator decides whether an account may use an OIDC client or a
// SAML SP.
type appAccessEvaluator interface {
	appaccess.OIDCAuthorizer
	appaccess.SAMLAuthorizer
}

// accountChooserQueries is the narrow surface the chooser reads applications
// through.
type accountChooserQueries interface {
	GetOIDCClient(ctx context.Context, clientID string) (db.OidcClient, error)
	GetSAMLSPByEntityID(ctx context.Context, entityID string) (db.SamlSp, error)
	GetEntityIconEtag(ctx context.Context, arg db.GetEntityIconEtagParams) (string, error)
}

func (s *Server) chooserQ() accountChooserQueries {
	if s.accountChooserOverride != nil {
		return s.accountChooserOverride
	}
	return s.queries
}

func (s *Server) chooserAccess() appAccessEvaluator {
	if s.appAccess == nil && s.queries != nil {
		s.appAccess = appaccess.NewService(s.queries)
	}
	return s.appAccess
}

// chooserApp is the application a ticket is for, with the access check for
// it. A ticket whose application has since gone answers
// account_chooser_expired.
type chooserApp struct {
	view    contract.AccountChooserApp
	allowed func(ctx context.Context, accountID int32) (bool, error)
}

func (s *Server) chooserAppFor(ctx context.Context, t *authn.AccountChooserTicket) (*chooserApp, error) {
	q := s.chooserQ()
	access := s.chooserAccess()
	gone := func(err error) error {
		if errors.Is(err, pgx.ErrNoRows) {
			return authn.ErrAccountChooserExpired()
		}
		return err
	}
	switch t.Kind {
	case authn.ChooserKindOIDC, authn.ChooserKindRequireIdentity:
		client, err := q.GetOIDCClient(ctx, t.App)
		if err != nil {
			return nil, gone(err)
		}
		app := &chooserApp{
			view: contract.AccountChooserApp{
				Kind: "oidc", Key: "oidc:" + client.ClientID, Name: client.DisplayName,
				IconURL: nonEmpty(uploadedIconOr(ctx, q, "oidc_client", client.ClientID, textOrEmpty(client.LogoUri))),
			},
			allowed: func(ctx context.Context, accountID int32) (bool, error) {
				d, err := access.EvaluateOIDC(ctx, accountID, client.ClientID)
				return d.Allowed, gone(err)
			},
		}
		return app, nil
	case authn.ChooserKindSAML, authn.ChooserKindSAMLInit:
		sp, err := q.GetSAMLSPByEntityID(ctx, t.App)
		if err != nil {
			return nil, gone(err)
		}
		app := &chooserApp{
			view: contract.AccountChooserApp{
				Kind: "saml", Key: "saml:" + sp.EntityID, Name: sp.DisplayName,
				IconURL: nonEmpty(uploadedIconOr(ctx, q, "saml_sp", strconv.FormatInt(sp.ID, 10), "")),
			},
			allowed: func(ctx context.Context, accountID int32) (bool, error) {
				d, err := access.EvaluateSAML(ctx, accountID, sp.ID)
				return d.Allowed, gone(err)
			},
		}
		return app, nil
	}
	return nil, authn.ErrAccountChooserExpired()
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// otherAccountURL is the sign-in page that adds an account and continues the
// ticket's request as it.
func otherAccountURL(nonce, loginHint string) string {
	u := "/login?return_to=" + url.QueryEscape(accountChooserContinuePath+"?ticket="+url.QueryEscape(nonce))
	if loginHint != "" {
		u += "&login_hint=" + url.QueryEscape(loginHint)
	}
	return u
}

type accountChooserInput struct {
	Ticket string `query:"ticket"`
}

type accountChooserOutput struct {
	Body contract.AccountChooser
}

// handleGetAccountChooser lists the browser's accounts for a chooser ticket,
// each marked with whether it may use the application. Public: the reader
// may have no account that can, and the page still offers "use another
// account".
func (s *Server) handleGetAccountChooser(ctx context.Context, in *accountChooserInput) (*accountChooserOutput, error) {
	t, err := authn.GetAccountChooser(ctx, s.kvStore, in.Ticket)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, authErrToHuma(authn.ErrAccountChooserExpired())
	}
	app, err := s.chooserAppFor(ctx, t)
	if err != nil {
		return nil, authErrToHuma(err)
	}
	out := &accountChooserOutput{Body: contract.AccountChooser{
		App:             app.view,
		Accounts:        []contract.AccountChooserAccount{},
		LoginHint:       t.LoginHint,
		OtherAccountURL: otherAccountURL(in.Ticket, t.LoginHint),
	}}
	b := sessstore.BrowserFromContext(ctx)
	if b == nil {
		return out, nil
	}
	sessions := b.Accounts()
	shorts := s.refs().Shorts(b.AccountIDs())
	for _, sess := range sessions {
		allowed, err := app.allowed(ctx, sess.Account.ID)
		if err != nil {
			return nil, authErrToHuma(err)
		}
		v := s.browserAccountView(sess.Account, shorts[sess.Account.ID])
		out.Body.Accounts = append(out.Body.Accounts, contract.AccountChooserAccount{
			AccountID: v.AccountID, U: v.U, Username: v.Username, DisplayName: v.DisplayName,
			AvatarURL: v.AvatarURL, Allowed: allowed,
		})
	}
	return out, nil
}

type chooseAccountInput struct {
	Body contract.ChooseAccountInput
}

type accountChooserRedirectOutput struct {
	Body contract.AccountChooserRedirect
}

// handleChooseAccount records the reader's choice on the ticket and returns
// the protocol address to resume with account_choice. The ticket can be
// chosen again within its lifetime.
func (s *Server) handleChooseAccount(ctx context.Context, in *chooseAccountInput) (*accountChooserRedirectOutput, error) {
	nonce := in.Body.Ticket
	t, err := authn.GetAccountChooser(ctx, s.kvStore, nonce)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, authErrToHuma(authn.ErrAccountChooserExpired())
	}
	b := sessstore.BrowserFromContext(ctx)
	if b == nil || !b.Has(in.Body.AccountID) {
		return nil, authErrToHuma(authn.ErrAccountNotSignedIn())
	}
	app, err := s.chooserAppFor(ctx, t)
	if err != nil {
		return nil, authErrToHuma(err)
	}
	allowed, err := app.allowed(ctx, in.Body.AccountID)
	if err != nil {
		return nil, authErrToHuma(err)
	}
	if !allowed {
		return nil, authErrToHuma(authn.ErrAppAccessDenied())
	}
	t, err = authn.BindAccountChooser(ctx, s.kvStore, nonce, in.Body.AccountID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, authErrToHuma(authn.ErrAccountChooserExpired())
	}
	return &accountChooserRedirectOutput{Body: contract.AccountChooserRedirect{
		Redirect: authn.ResumeWithChoice(t.ResumeURL, nonce),
	}}, nil
}

// handleAccountChooserContinueHTTP is where "use another account" lands after
// signing in (a full-page navigation): with the new account bound to the
// ticket and still signed in, the protocol request resumes as it; otherwise
// the reader picks on the chooser page again.
func (s *Server) handleAccountChooserContinueHTTP(w http.ResponseWriter, r *http.Request) {
	nonce := r.URL.Query().Get("ticket")
	t, err := authn.GetAccountChooser(r.Context(), s.kvStore, nonce)
	if err != nil {
		redirectAuthErrToError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if t == nil {
		http.Redirect(w, r, "/error?reason=account_chooser_expired", http.StatusFound)
		return
	}
	if t.AccountID != 0 && s.browser(r).Has(t.AccountID) {
		http.Redirect(w, r, authn.ResumeWithChoice(t.ResumeURL, nonce), http.StatusFound)
		return
	}
	http.Redirect(w, r, accountChooserPagePath+"?ticket="+url.QueryEscape(nonce), http.StatusFound)
}

type switchConsentAccountInput struct {
	Body contract.SwitchConsentAccountInput
}

// handleSwitchConsentAccount opens the account chooser for the request a
// consent page shows ("use another account"). The consent ticket is read,
// not consumed, and must belong to the account the page acts as; nothing is
// signed out.
func (s *Server) handleSwitchConsentAccount(ctx context.Context, in *switchConsentAccountInput) (*accountChooserRedirectOutput, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil || sess.Data == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	var ticket authn.AccountChooserTicket
	switch in.Body.Kind {
	case "oidc":
		consent, err := authn.LookupConsent(ctx, s.kvStore, in.Body.ConsentTicket)
		if err != nil {
			return nil, err
		}
		if err := consentTicketCheck(consent != nil, consent != nil && consent.AccountID == sess.Data.AccountID); err != nil {
			return nil, authErrToHuma(err)
		}
		resume, ok := authorizeReturnFor(in.Body.ReturnTo, s, consent.ClientID)
		if !ok {
			return nil, authErrToHuma(authn.ErrBadRequest())
		}
		ticket = authn.AccountChooserTicket{Kind: authn.ChooserKindOIDC, App: consent.ClientID, ResumeURL: resume}
	case "saml":
		consent, err := authn.LookupSAMLConsent(ctx, s.kvStore, in.Body.ConsentTicket)
		if err != nil {
			return nil, err
		}
		if err := consentTicketCheck(consent != nil, consent != nil && consent.AccountID == sess.Data.AccountID); err != nil {
			return nil, authErrToHuma(err)
		}
		ticket = authn.AccountChooserTicket{
			Kind: authn.ChooserKindSAML, App: consent.EntityID,
			ResumeURL: "/saml/sso/resume?ticket=" + url.QueryEscape(in.Body.ConsentTicket),
		}
	default:
		return nil, authErrToHuma(authn.ErrBadRequest())
	}
	nonce, err := authn.CreateAccountChooser(ctx, s.kvStore, ticket)
	if err != nil {
		return nil, err
	}
	return &accountChooserRedirectOutput{Body: contract.AccountChooserRedirect{
		Redirect: accountChooserPagePath + "?ticket=" + url.QueryEscape(nonce),
	}}, nil
}

// consentTicketCheck answers a consent ticket as the consent pages do: gone is
// invalid_consent_ticket; one for another account than the page acts as is
// account_not_signed_in, so the page signs that account in rather than
// showing it to this one.
func consentTicketCheck(exists, sameAccount bool) error {
	switch {
	case !exists:
		return authn.ErrInvalidConsentTicket()
	case !sameAccount:
		return authn.ErrAccountNotSignedIn()
	}
	return nil
}

// authorizeReturnFor validates the authorize address an OIDC consent page
// returns to: same origin, /oauth/authorize, for clientID. It returns the
// relative address without an earlier account choice.
func authorizeReturnFor(raw string, s *Server, clientID string) (string, bool) {
	p, ok := resolveReturnTo(raw, s.config)
	if !ok {
		return "", false
	}
	u, err := url.Parse(p)
	if err != nil || u.Path != "/oauth/authorize" || u.Query().Get("client_id") != clientID {
		return "", false
	}
	resume := u.Path
	if q := authn.StripQueryParams(u.RawQuery, authn.AccountChoiceParam, "reauth"); q != "" {
		resume += "?" + q
	}
	return resume, true
}

// bindChooserTicket binds a newly signed-in account to the account chooser
// ticket when returnTo is the chooser's continue address ("use another
// account"), whichever way the reader signed in. A failure only costs the
// shortcut: continue then shows the chooser again.
func (s *Server) bindChooserTicket(r *http.Request, returnTo string, accountID int32) {
	p, ok := resolveReturnTo(returnTo, s.config)
	if !ok {
		return
	}
	u, err := url.Parse(p)
	if err != nil || u.Path != accountChooserContinuePath {
		return
	}
	nonce := u.Query().Get("ticket")
	if nonce == "" {
		return
	}
	if _, err := authn.BindAccountChooser(r.Context(), s.kvStore, nonce, accountID); err != nil {
		logx.WithContext(r.Context()).WithError(err).WithFields(logrus.Fields{
			"event":      "account_chooser.bind_failed",
			"account_id": accountID,
		}).Warn("auth")
	}
}
