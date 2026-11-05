package saml

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/session"
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
func (i *IdP) maybeDemandSAMLConsent(w http.ResponseWriter, r *http.Request, account db.Account, sp db.SamlSp, acsURL, inResponseTo, relayState string, forceAuthn bool) (redirected bool, err error) {
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
		ForceAuthn:   forceAuthn,
	})
	if derr != nil {
		return false, derr
	}
	http.Redirect(w, r, i.withAccountRef(r, i.baseURL()+"/saml-consent?ticket="+url.QueryEscape(nonce), account.ID), http.StatusFound)
	return true, nil
}

// HandleConsentResume (GET /saml/sso/resume?ticket=…) completes a SAML login
// after the user approved the advisory consent screen. It re-checks
// authorization, records the acknowledgement, and emits the assertion from the
// stashed (gate-time-validated) issue context — so it works for every binding,
// including POST-binding SP-initiated SSO where the original request body cannot
// be replayed by the browser.
//
// The ticket names the account it was made for, and that account's session
// answers it rather than the browser's default one. "Use another account" on
// the consent page comes back here with an account_choice naming a different
// account signed in on this browser; the request then continues as that
// account (resumeSwitched).
func (i *IdP) HandleConsentResume(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nonce := r.URL.Query().Get("ticket")
	pending, err := authn.ReadSAMLConsent(ctx, i.kv, nonce)
	if err != nil {
		i.errorPage(w, r, "server_error", "consent_consume", err)
		return
	}
	sess, switched := i.resumeSession(r, pending)
	if sess == nil || sess.Data == nil || sess.Account == nil || sess.Account.Disabled {
		returnTo := i.baseURL() + r.URL.RequestURI()
		http.Redirect(w, r, i.baseURL()+"/login?return_to="+url.QueryEscape(returnTo), http.StatusFound)
		return
	}
	if i.maintenanceDenies(ctx, *sess.Account) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if switched {
		i.resumeSwitched(w, r, nonce, pending, sess)
		return
	}
	ticket, ok, err := authn.ConsumeSAMLConsent(ctx, i.kv, nonce, sess.Data.AccountID)
	if err != nil {
		i.errorPage(w, r, "server_error", "consent_consume", err)
		return
	}
	if !ok {
		i.errorPage(w, r, "saml_request_invalid", "consent_ticket_invalid", nil)
		return
	}
	sp, ok := i.consentTicketSP(w, r, ticket)
	if !ok {
		return
	}
	decision, ok := i.consentAccess(w, r, sess, sp)
	if !ok {
		return
	}
	// Record the advisory acknowledgement, then issue.
	if uerr := i.queries.UpsertSAMLConsent(ctx, db.UpsertSAMLConsentParams{AccountID: sess.Data.AccountID, SpID: sp.ID}); uerr != nil {
		i.errorPage(w, r, "server_error", "consent_record", uerr)
		return
	}
	row, err := i.queries.GetSession(ctx, sess.Data.SessionID)
	if err != nil {
		i.errorPage(w, r, "server_error", "session_load", err)
		return
	}
	i.issueAssertion(w, r, *sess.Account, sp, ticket.ACSURL, ticket.InResponseTo, ticket.RelayState, row.AuthTime.Time, sess.Data.SessionID, "sso_consent", decision.ExposedGroupSlugs())
}

// resumeSession picks the session a consent resume acts as: the account an
// account_choice for this SP names when it differs from the ticket's and is
// signed in here (switched), otherwise the ticket's own account.
func (i *IdP) resumeSession(r *http.Request, pending *authn.SAMLConsentTicket) (sess *authn.Session, switched bool) {
	ctx := r.Context()
	b := session.BrowserFromContext(ctx)
	if b == nil || pending == nil {
		return authn.SessionFromContext(ctx), false
	}
	chosen := authn.ChosenAccount(ctx, i.kv, r.URL.Query().Get(accountChoiceParam), authn.ChooserKindSAML, pending.EntityID)
	if chosen != 0 && chosen != pending.AccountID {
		if s := b.ForAccount(chosen); s != nil {
			return s, true
		}
	}
	return b.ForAccount(pending.AccountID), false
}

// resumeSwitched continues a consent-paused request as another account
// (api.md 6): the original ticket is spent, the new account's access is
// checked, and it either is refused, is asked for its own acknowledgement, or
// gets the assertion. A request that demanded ForceAuthn has the new account
// re-authenticate first, before the original ticket is spent, so the sign-in
// comes back to the same resume address.
func (i *IdP) resumeSwitched(w http.ResponseWriter, r *http.Request, nonce string, pending *authn.SAMLConsentTicket, sess *authn.Session) {
	ctx := r.Context()
	row, err := i.queries.GetSession(ctx, sess.Data.SessionID)
	if err != nil {
		i.errorPage(w, r, "server_error", "session_load", err)
		return
	}
	authTime := row.AuthTime.Time
	if pending.ForceAuthn {
		satisfied := false
		if reauthNonce := r.URL.Query().Get("reauth"); reauthNonce != "" {
			ok, cerr := authn.ConsumeReauth(ctx, i.kv, "saml:reauth:", reauthNonce, sess.Data.AccountID, authTime)
			if cerr != nil {
				i.errorPage(w, r, "server_error", "reauth_consume", cerr)
				return
			}
			satisfied = ok
		}
		if !satisfied {
			reauth, derr := authn.DemandReauth(ctx, i.kv, "saml:reauth:", sess.Data.AccountID)
			if derr != nil {
				i.errorPage(w, r, "server_error", "reauth_demand", derr)
				return
			}
			kept := append(rawQueryWithout(r.URL.RawQuery, "reauth"), "reauth="+reauth)
			ret := i.baseURL() + r.URL.Path + "?" + strings.Join(kept, "&")
			http.Redirect(w, r, i.baseURL()+"/login?return_to="+url.QueryEscape(ret)+"&login_hint="+url.QueryEscape(sess.Account.Username), http.StatusFound)
			return
		}
	}
	ticket, ok, err := authn.ConsumeSAMLConsent(ctx, i.kv, nonce, pending.AccountID)
	if err != nil {
		i.errorPage(w, r, "server_error", "consent_consume", err)
		return
	}
	if !ok {
		i.errorPage(w, r, "saml_request_invalid", "consent_ticket_invalid", nil)
		return
	}
	sp, ok := i.consentTicketSP(w, r, ticket)
	if !ok {
		return
	}
	decision, ok := i.consentAccess(w, r, sess, sp)
	if !ok {
		return
	}
	if redirected, cerr := i.maybeDemandSAMLConsent(w, r, *sess.Account, sp, ticket.ACSURL, ticket.InResponseTo, ticket.RelayState, ticket.ForceAuthn); cerr != nil {
		i.errorPage(w, r, "server_error", "consent_stash", cerr)
		return
	} else if redirected {
		return
	}
	i.issueAssertion(w, r, *sess.Account, sp, ticket.ACSURL, ticket.InResponseTo, ticket.RelayState, authTime, sess.Data.SessionID, "sso_consent", decision.ExposedGroupSlugs())
}

// consentTicketSP loads the SP a consent ticket was made for; a removed or
// disabled SP ends on the error page (ok=false).
func (i *IdP) consentTicketSP(w http.ResponseWriter, r *http.Request, ticket *authn.SAMLConsentTicket) (db.SamlSp, bool) {
	sp, err := i.queries.GetSAMLSPByID(r.Context(), ticket.SPID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			i.errorPage(w, r, "saml_sp_unknown", "sp_unknown", fmt.Errorf("%w: no SP registered for ticket SPID %d", ErrUnknownSP, ticket.SPID))
		} else {
			i.errorPage(w, r, "server_error", "sp_lookup", err)
		}
		return db.SamlSp{}, false
	}
	if sp.Disabled {
		i.errorPage(w, r, "saml_sp_unknown", "sp_unknown", fmt.Errorf("%w: SP %q is registered but disabled", ErrUnknownSP, sp.EntityID))
		return db.SamlSp{}, false
	}
	return sp, true
}

// consentAccess re-checks per-app access at resume — it may have changed since
// the gate, or the account may be another one. Fails closed; a denial is
// audited and ends on the access page (ok=false).
func (i *IdP) consentAccess(w http.ResponseWriter, r *http.Request, sess *authn.Session, sp db.SamlSp) (appaccess.Decision, bool) {
	ctx := r.Context()
	decision, accessErr := i.evaluateSAMLAccess(ctx, sess.Data.AccountID, sp.ID)
	if accessErr != nil {
		i.errorPage(w, r, "server_error", "access_evaluate", accessErr)
		return decision, false
	}
	if !decision.Allowed {
		acctID := sess.Data.AccountID
		audit.RecordOrLog(ctx, i.audit, audit.Record{
			AccountID: &acctID, Factor: audit.FactorSAMLSP, Event: audit.EventAccessDenied,
			IP: audit.ParseIPOrNil(i.auditIP(r)), UserAgent: r.UserAgent(),
			Detail: map[string]any{"reason": samlAccessAuditReason(decision.Source), "sp": sp.EntityID},
		})
		http.Redirect(w, r, i.withAccountRef(r, i.baseURL()+"/error?reason=app_access_denied&app="+url.QueryEscape(sp.DisplayName), acctID), http.StatusFound)
		return decision, false
	}
	return decision, true
}
