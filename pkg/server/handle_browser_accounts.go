package server

import (
	"context"

	"prohibitorum/pkg/avatar"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	sessstore "prohibitorum/pkg/session"
)

type browserAccountsOutput struct {
	Body contract.BrowserAccounts
}

// handleListBrowserAccounts lists the accounts signed in on this browser in
// the fixed sign-in order. It ignores X-Prohibitorum-Account: the list is the
// browser's, not one account's.
func (s *Server) handleListBrowserAccounts(ctx context.Context, _ *struct{}) (*browserAccountsOutput, error) {
	out := &browserAccountsOutput{Body: contract.BrowserAccounts{
		Limit:    s.browserSessions().Limit(),
		Accounts: []contract.BrowserAccount{},
	}}
	b := sessstore.BrowserFromContext(ctx)
	if b == nil {
		return out, nil
	}
	sessions := b.Accounts()
	ids := make([]int32, len(sessions))
	for i, sess := range sessions {
		ids[i] = sess.Account.ID
	}
	shorts := s.refs().Shorts(ids)
	for _, sess := range sessions {
		out.Body.Accounts = append(out.Body.Accounts, s.browserAccountView(sess.Account, shorts[sess.Account.ID]))
	}
	return out, nil
}

func (s *Server) browserAccountView(a *db.Account, u string) contract.BrowserAccount {
	v := contract.BrowserAccount{
		AccountID:   a.ID,
		U:           u,
		Username:    a.Username,
		DisplayName: a.DisplayName,
	}
	origin := ""
	if s.config != nil && len(s.config.PublicOrigins) > 0 {
		origin = s.config.PublicOrigins[0]
	}
	if url := avatar.AccountURL(*a, origin); url != "" {
		v.AvatarURL = &url
	}
	return v
}
