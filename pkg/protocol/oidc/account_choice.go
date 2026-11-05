package oidc

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/session"
)

// chooserPagePath is the console page that lists the browser's accounts for
// one protocol request.
const chooserPagePath = "/choose-account"

// accountRequest is what a protocol request says about the account it wants.
type accountRequest struct {
	kind string
	// app is the client_id the request is for; a choice made for another
	// application is ignored.
	app    string
	choice string
	// hint is the verified id_token_hint's claims, nil when none was sent.
	hint          map[string]any
	loginHint     string
	selectAccount bool
}

// accountOutcome is where account resolution leaves a request.
type accountOutcome int

const (
	accountResolved accountOutcome = iota
	// accountNeedsLogin: nothing signed in here (or nothing usable).
	accountNeedsLogin
	// accountNeedsChoice: the reader picks on the chooser page.
	accountNeedsChoice
)

type accountResolution struct {
	outcome accountOutcome
	sess    *authn.Session
	// prefill is the username the login and chooser pages pre-fill: the
	// request's login_hint, else the account id_token_hint names.
	prefill string
	// hinted is the account id_token_hint names when it could be found.
	hinted *db.Account
}

// usableSession reports whether sess can act for a protocol request: the
// disabled-mid-session sentinel (no Data) and a disabled account cannot.
func usableSession(sess *authn.Session) bool {
	return sess != nil && sess.Data != nil && (sess.Account == nil || !sess.Account.Disabled)
}

// browserAccounts returns the accounts signed in on the request's browser in
// the fixed order. A handler run without LoadSession (unit tests) sees just
// the session on the context.
func browserAccounts(r *http.Request) []*authn.Session {
	if b := session.BrowserFromContext(r.Context()); b != nil {
		return b.Accounts()
	}
	if sess := authn.SessionFromContext(r.Context()); usableSession(sess) {
		return []*authn.Session{sess}
	}
	return nil
}

// browserUnsettled reports an account on the browser whose session could not
// be read this request, so "the only account" may not be the only one.
func browserUnsettled(r *http.Request) bool {
	b := session.BrowserFromContext(r.Context())
	return b != nil && b.Unsettled()
}

// resolveAccount fixes the account a protocol request acts as (design 5.3):
//
//  1. a valid account_choice for this kind and application;
//  2. unless prompt=select_account: the session id_token_hint's sid names,
//     then the account it names, then a login_hint matching exactly one
//     account's username or verified email — only among this browser's
//     accounts;
//  3. prompt=select_account opens the chooser;
//  4. no account: sign in;
//  5. one account: that one;
//  6. several: the chooser.
//
// prompt=none is the caller's concern: it maps accountNeedsLogin and
// accountNeedsChoice to its protocol's errors.
func (p *Provider) resolveAccount(r *http.Request, req accountRequest) accountResolution {
	res := p.resolveAccountOnce(r, req)
	if res.outcome == accountResolved && res.sess != nil && res.sess.Account != nil {
		// Signing in to an application is activity of the account chosen,
		// and only of it.
		if b := session.BrowserFromContext(r.Context()); b != nil {
			b.Touch(res.sess.Account.ID)
		}
	}
	return res
}

func (p *Provider) resolveAccountOnce(r *http.Request, req accountRequest) accountResolution {
	ctx := r.Context()
	accounts := browserAccounts(r)
	find := func(id int32) *authn.Session {
		for _, s := range accounts {
			if s.Data.AccountID == id {
				return s
			}
		}
		return nil
	}
	res := accountResolution{prefill: req.loginHint}
	// Looked up first: callers compare the account they end up with against
	// the one the hint names, however it was chosen.
	if req.hint != nil {
		if acct, err := accountFromTokenClaims(ctx, p.queries, req.hint); err == nil {
			res.hinted = &acct
			if res.prefill == "" {
				res.prefill = acct.Username
			}
		}
	}
	resolved := func(s *authn.Session) accountResolution {
		return accountResolution{outcome: accountResolved, sess: s, prefill: res.prefill, hinted: res.hinted}
	}

	if id := authn.ChosenAccount(ctx, p.kv, req.choice, req.kind, req.app); id != 0 {
		if s := find(id); s != nil {
			return resolved(s)
		}
	}

	if !req.selectAccount {
		if sid, _ := req.hint["sid"].(string); sid != "" {
			for _, s := range accounts {
				if s.Data.SessionID == sid {
					return resolved(s)
				}
			}
		}
		if res.hinted != nil {
			if s := find(res.hinted.ID); s != nil {
				return resolved(s)
			}
		}
		if req.loginHint != "" {
			if s := p.matchLoginHint(ctx, accounts, req.loginHint); s != nil {
				return resolved(s)
			}
		}
	}

	switch {
	case len(accounts) == 0:
		res.outcome = accountNeedsLogin
	case req.selectAccount || len(accounts) > 1 || browserUnsettled(r):
		res.outcome = accountNeedsChoice
	default:
		res.outcome = accountResolved
		res.sess = accounts[0]
	}
	return res
}

// matchLoginHint returns the one account whose username or verified email is
// hint, compared without case, or nil when none or several match.
func (p *Provider) matchLoginHint(ctx context.Context, accounts []*authn.Session, hint string) *authn.Session {
	var match *authn.Session
	for _, s := range accounts {
		if s.Account == nil || !p.accountAnswersTo(ctx, s.Account, hint) {
			continue
		}
		if match != nil {
			return nil
		}
		match = s
	}
	return match
}

func (p *Provider) accountAnswersTo(ctx context.Context, acct *db.Account, hint string) bool {
	if strings.EqualFold(acct.Username, hint) {
		return true
	}
	if !strings.Contains(hint, "@") {
		return false
	}
	row, err := p.queries.GetAccountEmailByAddress(ctx, db.GetAccountEmailByAddressParams{AccountID: acct.ID, Lower: hint})
	return err == nil && row.VerifiedAt.Valid
}

// redirectToChooser stores an account chooser ticket for this request and
// sends the browser to the chooser page. resumePath is the request's own
// same-origin address; any earlier choice and re-auth nonce are dropped from
// it, since the new choice is appended when the reader picks.
func (p *Provider) redirectToChooser(w http.ResponseWriter, r *http.Request, kind, app, loginHint string) error {
	resume := r.URL.Path
	if q := authn.StripQueryParams(r.URL.RawQuery, authn.AccountChoiceParam, "reauth"); q != "" {
		resume += "?" + q
	}
	nonce, err := authn.CreateAccountChooser(r.Context(), p.kv, authn.AccountChooserTicket{
		Kind: kind, App: app, ResumeURL: resume, LoginHint: loginHint,
	})
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, p.cfg.OIDC.Issuer+chooserPagePath+"?ticket="+url.QueryEscape(nonce), http.StatusFound)
	return nil
}

// loginURL is the sign-in page that returns to returnTo, pre-filling
// loginHint when there is one.
func (p *Provider) loginURL(returnTo, loginHint string) string {
	u := p.cfg.OIDC.Issuer + "/login?return_to=" + url.QueryEscape(returnTo)
	if loginHint != "" {
		u += "&login_hint=" + url.QueryEscape(loginHint)
	}
	return u
}

// accountRef is accountID's `u` on the request's browser, or "" when the
// browser holds at most one account: a console page opened from a protocol
// flow then acts as the chosen account, not the default one.
func (p *Provider) accountRef(r *http.Request, accountID int32) string {
	b := session.BrowserFromContext(r.Context())
	if b == nil || p.refs == nil {
		return ""
	}
	ids := b.AccountIDs()
	if len(ids) <= 1 {
		return ""
	}
	return p.refs.Shorts(ids)[accountID]
}

// withAccountRef appends u=<accountID's u> to a console URL that already has
// a query, when the browser holds several accounts.
func (p *Provider) withAccountRef(r *http.Request, consoleURL string, accountID int32) string {
	if u := p.accountRef(r, accountID); u != "" {
		sep := "&"
		if !strings.Contains(consoleURL, "?") {
			sep = "?"
		}
		return consoleURL + sep + "u=" + url.QueryEscape(u)
	}
	return consoleURL
}
