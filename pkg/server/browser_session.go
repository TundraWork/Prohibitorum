package server

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	sessstore "prohibitorum/pkg/session"
)

// browserSessions returns the manager for the browser's signed-in accounts.
// Tests that build a bare Server get one over their session store.
func (s *Server) browserSessions() *sessstore.Manager {
	if s.sessions != nil {
		return s.sessions
	}
	var ipOf func(*http.Request) string
	if s.clientIP != nil {
		ipOf = s.clientIP.IP
	}
	var q sessstore.AccountQueries
	if s.accountLookup != nil {
		q = s.accountLookup
	} else if s.queries != nil {
		q = s.queries
	}
	s.sessions = sessstore.NewManager(s.config, q, s.sessionStore, ipOf)
	return s.sessions
}

// browser returns the accounts signed in on the request's browser.
func (s *Server) browser(r *http.Request) *sessstore.Browser {
	return s.browserSessions().BrowserFor(r)
}

// refs returns the short account references; tests without the instance key
// get a fixed one.
func (s *Server) refs() *sessstore.AccountRefs {
	if s.accountRefs == nil {
		s.accountRefs = sessstore.NewAccountRefs([]byte("prohibitorum-test-account-ref-key"))
	}
	return s.accountRefs
}

// accountShorts returns the `u` of every account signed in on the request's
// browser, computed over that set so colliding prefixes widen to 8 digits.
func (s *Server) accountShorts(r *http.Request) map[int32]string {
	return s.refs().Shorts(s.browser(r).AccountIDs())
}

// shortFor returns accountID's `u` on this browser, or "" when the browser
// holds at most one account: with a single account no `u` appears anywhere.
func (s *Server) shortFor(r *http.Request, accountID int32) string {
	b := s.browser(r)
	if b.Count() <= 1 {
		return ""
	}
	return s.accountShorts(r)[accountID]
}

// withAccountRef adds `u` for accountID to a console path when the browser
// holds more than one account, so the page the server sends the reader to
// acts as that account rather than the default one. Protocol and API paths
// are returned unchanged.
func (s *Server) withAccountRef(r *http.Request, path string, accountID int32) string {
	if !isConsolePath(path) {
		return path
	}
	u := s.shortFor(r, accountID)
	if u == "" {
		return path
	}
	return setQueryParam(path, "u", u)
}

func isConsolePath(path string) bool {
	if !strings.HasPrefix(path, "/") {
		return false
	}
	for _, prefix := range []string{"/oauth/", "/oidc/", "/saml/", "/api/", "/.well-known/"} {
		if strings.HasPrefix(path, prefix) {
			return false
		}
	}
	return true
}

// setQueryParam sets key=value on a relative path, keeping its other query
// parameters and fragment.
func setQueryParam(path, key, value string) string {
	u, err := url.Parse(path)
	if err != nil {
		return path
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
}

// issueBrowserSession signs accountID in on the request's browser: a session
// is issued and its entry written into the session cookies (design 3.2). An
// account already signed in here is replaced in place; a new one is refused
// with account_limit_reached when the browser is at its cap, before anything
// is issued. When returnTo is the account chooser's continue address, the
// account is bound to that chooser ticket so continuing resumes the protocol
// flow as it.
func (s *Server) issueBrowserSession(w http.ResponseWriter, r *http.Request, p sessstore.AttachParams, returnTo string) (*authn.Session, error) {
	sess, err := s.browser(r).Attach(w, p)
	if errors.Is(err, sessstore.ErrAccountLimit) {
		return nil, authn.ErrAccountLimitReached()
	}
	if err != nil {
		return nil, err
	}
	s.bindChooserTicket(r, returnTo, p.AccountID)
	return sess, nil
}

// bindChooserTicket binds a newly signed-in account to the account chooser
// ticket when returnTo is the chooser's continue address ("use another
// account"), whichever way the reader signed in.
func (s *Server) bindChooserTicket(r *http.Request, returnTo string, accountID int32) {
	// Filled in with the account chooser (handle_account_chooser.go).
	_, _, _ = r, returnTo, accountID
}

// canSignIn reports whether accountID can be signed in on this browser:
// it already is, or the browser is below its cap.
func (s *Server) canSignIn(r *http.Request, accountID int32) bool {
	return s.browser(r).CanAttach(accountID)
}

// loginResult builds the sign-in completion body: the validated return
// address, with the new account's `u` added when it is a console page and the
// browser now holds several accounts (api.md 5).
func (s *Server) loginResult(r *http.Request, accountID int32, rawReturnTo string) contract.LoginResult {
	redirect := validateReturnTo(rawReturnTo, s.config)
	return contract.LoginResult{
		Redirect: s.withAccountRef(r, redirect, accountID),
		Account:  s.signedInAccount(r, accountID),
	}
}

// signedInAccount names the account a sign-in just added, with its `u` on
// this browser.
func (s *Server) signedInAccount(r *http.Request, accountID int32) *contract.SignedInAccount {
	return &contract.SignedInAccount{AccountID: accountID, U: s.accountShorts(r)[accountID]}
}
