// Package server — handle_admin_account_tokens.go
//
// Admin endpoints for inspecting and revoking a user's personal access tokens.
//
//   GET  /accounts/{id}/tokens        admin (typed, via registerOp)
//   POST /accounts/tokens/revoke      admin + sudo (raw, via registerSudoOpHTTP)
//
// The revoke SQL (RevokePATByID) has NO account-ownership guard — it revokes any
// PAT by id. The route-level admin+sudo gate is therefore the ONLY protection and
// must never be relaxed. See Task 4 in the plan.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/logx"
)

// ----- GET /accounts/{id}/tokens (admin, typed) ------------------------------

type accountTokensOut struct {
	Body []contract.PersonalAccessTokenView
}

// handleListAccountTokens lists all non-revoked PATs for a given account.
// It reuses the same getAccountIn path-param struct that handleGetAccount and
// handleListAccountSessions use; huma parses the int32 id from the URL.
func (s *Server) handleListAccountTokens(ctx context.Context, in *getAccountIn) (*accountTokensOut, error) {
	q := s.patQueriesFn()
	// Account-existence guard (mirrors handleListAccountSessions /
	// handleListAccountCredentials): a garbage id must 404, not return 200+[].
	if _, err := q.GetAccountByID(ctx, in.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrAccountNotFound())
		}
		return nil, fmt.Errorf("handleListAccountTokens: load account: %w", err)
	}
	rows, err := q.ListPATsByAccount(ctx, in.ID)
	if err != nil {
		return nil, fmt.Errorf("handleListAccountTokens: %w", err)
	}
	out := make([]contract.PersonalAccessTokenView, 0, len(rows))
	for _, r := range rows {
		out = append(out, patView(r))
	}
	return &accountTokensOut{Body: out}, nil
}

// ----- POST /accounts/tokens/revoke (admin + sudo, raw) ----------------------

type revokeAccountTokenBody struct {
	ID int32 `json:"id"`
}

// handleRevokeAccountTokenHTTP revokes a PAT by its numeric id. The SQL has no
// account-ownership check so the admin+sudo route gate is the sole protection.
// Returns 404 when the id is unknown or already revoked.
func (s *Server) handleRevokeAccountTokenHTTP(w http.ResponseWriter, r *http.Request) {
	var body revokeAccountTokenBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == 0 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	n, err := s.queries.RevokePATByID(r.Context(), body.ID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleRevokeAccountToken: %w", err))
		return
	}
	if n == 0 {
		writeAuthErr(w, authn.ErrCredentialNotFound())
		return
	}
	actor := faActorID(r.Context())
	credRef := int64(body.ID)
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID:     actor,
		Factor:        audit.FactorPAT,
		Event:         audit.EventRevoke,
		CredentialRef: &credRef,
		Detail:        map[string]any{"actor": "admin"},
	})
	actorID := int32(0)
	if actor != nil {
		actorID = *actor
	}
	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event":    "auth.pat_revoked_admin",
		"actor_id": actorID,
		"pat_id":   body.ID,
	}).Info("auth")
	w.WriteHeader(http.StatusNoContent)
}
