package server

import (
	"context"
	"fmt"
	"net/url"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
)

type samlLaunchOutput struct {
	Body contract.SAMLLaunchResult
}

// handleLaunchMySAMLApp starts an IdP-initiated SAML sign-in from the
// launcher. /saml/sso/init is a full-page navigation that cannot carry the
// tab's account header, so this request (which can) binds an account chooser
// ticket to that account first; the navigation then opens the application as
// the tab's account (design 5.8).
func (s *Server) handleLaunchMySAMLApp(ctx context.Context, in *contract.SAMLLaunchInput) (*samlLaunchOutput, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil || sess.Account == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	if s.appLister == nil {
		return nil, fmt.Errorf("saml launch: app lister unavailable")
	}
	// The allowed-app list is the launcher's own view of policy: an SP
	// missing from it is unknown, disabled or refused to this account, and
	// all three answer alike.
	apps, err := s.appLister.ListAllowedApps(ctx, sess.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("saml launch: list allowed apps: %w", err)
	}
	allowed := false
	for _, app := range apps {
		if app.Ref.Kind == appaccess.KindSAML && app.EntityID == in.SP {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, authErrToHuma(authn.ErrAppAccessDenied())
	}
	launch := "/saml/sso/init?sp=" + url.QueryEscape(in.SP)
	nonce, err := authn.CreateAccountChooser(ctx, s.kvStore, authn.AccountChooserTicket{
		Kind:      authn.ChooserKindSAMLInit,
		App:       in.SP,
		ResumeURL: launch,
		AccountID: sess.Account.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("saml launch: account choice: %w", err)
	}
	return &samlLaunchOutput{Body: contract.SAMLLaunchResult{
		Redirect: launch + "&account_choice=" + url.QueryEscape(nonce),
	}}, nil
}
