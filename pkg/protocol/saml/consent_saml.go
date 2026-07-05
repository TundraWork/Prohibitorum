package saml

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// attributeLabels turns an SP's attribute_map (the ordered JSONB array of
// attrMapEntry) into a de-duplicated, order-preserving list of human labels for
// the advisory consent screen — FriendlyName when set, else the raw Name.
// Malformed/empty input yields no labels (the screen shows a generic fallback).
func attributeLabels(mapJSON []byte) []string {
	var entries []attrMapEntry
	if err := json.Unmarshal(mapJSON, &entries); err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(entries))
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		label := e.FriendlyName
		if label == "" {
			label = e.Name
		}
		if label == "" {
			continue
		}
		if _, dup := seen[label]; dup {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	return out
}

// maybeDemandSAMLConsent gates assertion issuance on a stored advisory ack.
// Returns redirected=true when it has written a 302 to /saml-consent (the caller
// MUST return); false to proceed with issuance. The already-validated issue
// context (acsURL, inResponseTo, relayState) is stashed in the ticket so the
// resume path can emit the assertion without the browser re-sending the original
// AuthnRequest — which is what makes POST-binding SP-initiated consent work.
func (i *IdP) maybeDemandSAMLConsent(w http.ResponseWriter, r *http.Request, account db.Account, sp db.SamlSp, acsURL, inResponseTo, relayState string) (redirected bool, err error) {
	has, herr := i.queries.HasSAMLConsent(r.Context(), db.HasSAMLConsentParams{AccountID: account.ID, SpID: sp.ID})
	if herr != nil {
		return false, herr
	}
	if has {
		return false, nil
	}
	nonce, derr := authn.DemandSAMLConsent(r.Context(), i.kv, authn.SAMLConsentTicket{
		AccountID:    account.ID,
		SPID:         sp.ID,
		EntityID:     sp.EntityID,
		DisplayName:  sp.DisplayName,
		Attributes:   attributeLabels(sp.AttributeMap),
		ACSURL:       acsURL,
		InResponseTo: inResponseTo,
		RelayState:   relayState,
	})
	if derr != nil {
		return false, derr
	}
	http.Redirect(w, r, i.baseURL()+"/saml-consent?ticket="+url.QueryEscape(nonce), http.StatusFound)
	return true, nil
}

// HandleConsentResume (GET /saml/sso/resume?ticket=…) completes a SAML login
// after the user approved the advisory consent screen. It re-checks
// authorization, records the acknowledgement, and emits the assertion from the
// stashed (gate-time-validated) issue context — so it works for every binding,
// including POST-binding SP-initiated SSO where the original request body cannot
// be replayed by the browser.
func (i *IdP) HandleConsentResume(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := authn.SessionFromContext(ctx)
	if sess == nil || sess.Data == nil || sess.Account == nil || sess.Account.Disabled {
		returnTo := i.baseURL() + r.URL.RequestURI()
		http.Redirect(w, r, i.baseURL()+"/login?return_to="+url.QueryEscape(returnTo), http.StatusFound)
		return
	}
	ticket, ok, err := authn.ConsumeSAMLConsent(ctx, i.kv, r.URL.Query().Get("ticket"), sess.Data.AccountID)
	if err != nil {
		i.errorPage(w, r, "server_error")
		return
	}
	if !ok {
		i.errorPage(w, r, "saml_request_invalid")
		return
	}
	sp, err := i.queries.GetSAMLSPByID(ctx, ticket.SPID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			i.errorPage(w, r, "saml_sp_unknown")
		} else {
			i.errorPage(w, r, "server_error")
		}
		return
	}
	if sp.Disabled {
		i.errorPage(w, r, "saml_sp_unknown")
		return
	}
	// Re-check per-app access — it may have changed since the gate. Fail closed.
	authzed, aerr := i.queries.IsAccountAuthorizedForSAMLSP(ctx, db.IsAccountAuthorizedForSAMLSPParams{
		AccountID: pgtype.Int4{Int32: sess.Data.AccountID, Valid: true},
		SpID:      sp.ID,
	})
	if aerr != nil {
		i.errorPage(w, r, "server_error")
		return
	}
	if !authzed.Bool {
		acctID := sess.Data.AccountID
		_ = i.audit.Record(ctx, audit.Record{
			AccountID: &acctID, Factor: audit.FactorSAMLSP, Event: audit.EventAccessDenied,
			IP: audit.ParseIPOrNil(i.auditIP(r)), UserAgent: r.UserAgent(),
			Detail: map[string]any{"reason": "app_access_denied", "sp": sp.EntityID},
		})
		http.Redirect(w, r, i.baseURL()+"/error?reason=app_access_denied&app="+url.QueryEscape(sp.DisplayName), http.StatusFound)
		return
	}
	// Record the advisory acknowledgement, then issue.
	if uerr := i.queries.UpsertSAMLConsent(ctx, db.UpsertSAMLConsentParams{AccountID: sess.Data.AccountID, SpID: sp.ID}); uerr != nil {
		i.errorPage(w, r, "server_error")
		return
	}
	row, err := i.queries.GetSession(ctx, sess.Data.SessionID)
	if err != nil {
		i.errorPage(w, r, "server_error")
		return
	}
	i.issueAssertion(w, r, *sess.Account, sp, ticket.ACSURL, ticket.InResponseTo, ticket.RelayState, row.AuthTime.Time, sess.Data.SessionID, "sso_consent")
}
