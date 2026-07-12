// Package server — handle_admin_account_tokens.go
//
// Admin endpoints for inspecting and revoking a user's personal access tokens.
//
//	GET  /accounts/{id}/tokens        admin (typed, via registerOp)
//	POST /accounts/tokens/revoke      admin + sudo (raw, via registerSudoOpHTTP)
//
// The revoke SQL (RevokePATByID) has NO account-ownership guard — it revokes any
// PAT by id. The route-level admin+sudo gate is therefore the ONLY protection and
// must never be relaxed. See Task 4 in the plan.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/logx"
)

// ----- GET /accounts/{id}/tokens — paginated in handle_nested_pagination.go ----

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
	// Load the PAT before revoking to capture the owner's account_id for the
	// audit record. RevokePATByID has no account-ownership guard (the admin
	// route gate is the sole protection), so this lookup must precede the
	// revoke. Failure to load (unknown / already-revoked id) is surfaced via
	// the n==0 check on the revoke result; we tolerate a pre-load miss
	// gracefully (ownerAccountID stays 0) rather than returning early, so
	// the revoke itself remains the canonical not-found signal.
	var ownerAccountID int32
	if pat, err := s.queries.GetPATByID(r.Context(), body.ID); err == nil {
		ownerAccountID = pat.AccountID
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
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID:     actor,
		Factor:        audit.FactorPAT,
		Event:         audit.EventRevoke,
		CredentialRef: &credRef,
		Detail:        map[string]any{"actor": "admin", "target_account_id": ownerAccountID},
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
