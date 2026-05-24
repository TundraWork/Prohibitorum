package saml

import (
	"context"
	"errors"
	"net/http"

	"prohibitorum/pkg/db"
)

var (
	ErrUnknownSP        = errors.New("saml: unknown service provider")
	ErrInvalidACS       = errors.New("saml: ACS URL does not match registered endpoints")
	ErrMissingSignature = errors.New("saml: SP signature required but absent")
)

type IdP struct {
	q db.Querier
}

func NewIdP(q db.Querier) *IdP {
	return &IdP{q: q}
}

// TODO(v0.5): render <EntityDescriptor> from signing_key + configx.SAML config.
func (i *IdP) Metadata(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "saml.Metadata: TODO(v0.5)", http.StatusNotImplemented)
}

// TODO(v0.5): parse AuthnRequest, validate against saml_sp config, require
// session (redirect to /login if absent), build signed Response.
func (i *IdP) SSO(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "saml.SSO: TODO(v0.5)", http.StatusNotImplemented)
}

// TODO(v0.5): parse LogoutRequest, propagate to other SPs via saml_session,
// revoke our session.
func (i *IdP) SLO(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "saml.SLO: TODO(v0.5)", http.StatusNotImplemented)
}

// TODO(v0.5): GetSAMLSubjectID; if absent, generate 32-byte random base64url,
// InsertSAMLSubjectID, return.
func (i *IdP) SubjectID(ctx context.Context, accountID int32, spID int64, format string) (string, error) {
	return "", ErrUnknownSP
}
