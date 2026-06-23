package saml

import (
	"encoding/json"
	"net/http"
	"net/url"

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
// MUST return); false to proceed with issuance. ReturnTo is the exact inbound
// SSO URL so the assertion flow resumes verbatim after the ack (the signed raw
// query is preserved, exactly like the forced-re-auth bounce).
func (i *IdP) maybeDemandSAMLConsent(w http.ResponseWriter, r *http.Request, accountID int32, sp db.SamlSp) (redirected bool, err error) {
	has, herr := i.queries.HasSAMLConsent(r.Context(), db.HasSAMLConsentParams{
		AccountID: accountID,
		SpID:      sp.ID,
	})
	if herr != nil {
		return false, herr
	}
	if has {
		return false, nil
	}
	nonce, derr := authn.DemandSAMLConsent(r.Context(), i.kv, authn.SAMLConsentTicket{
		AccountID:   accountID,
		SPID:        sp.ID,
		EntityID:    sp.EntityID,
		DisplayName: sp.DisplayName,
		Attributes:  attributeLabels(sp.AttributeMap),
		ReturnTo:    i.baseURL() + r.URL.RequestURI(),
	})
	if derr != nil {
		return false, derr
	}
	http.Redirect(w, r, i.baseURL()+"/saml-consent?ticket="+url.QueryEscape(nonce), http.StatusFound)
	return true, nil
}
