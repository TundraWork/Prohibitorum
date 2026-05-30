package saml

import (
	"encoding/pem"
	"errors"

	"prohibitorum/pkg/db"

	"github.com/jackc/pgx/v5/pgtype"
)

// persistentNameIDFormat11 is the SAML 1.1 persistent NameID format, the
// default this IdP issues when an SP does not override it.
const persistentNameIDFormat11 = "urn:oasis:names:tc:SAML:1.1:nameid-format:persistent"

// SPOptions describes the operator-supplied inputs for registering a new SAML
// service provider via the CLI. Metadata (when present) is parsed for the
// entity_id, ACS endpoints, and signing certs; the explicit override flags then
// win over whatever was parsed.
type SPOptions struct {
	// MetadataXML is the raw SP metadata document. When non-empty it is parsed
	// for entity_id + ACS + signing certs.
	MetadataXML []byte

	// Override flags. EntityID/DisplayName/NameIDFormat override the parsed (or
	// default) values when non-empty. Kind selects the attribute-map profile.
	EntityID     string
	DisplayName  string
	Kind         string // "ghes" or "generic" (empty == generic)
	NameIDFormat string

	// RequireSignedAuthnRequest is applied verbatim. Note Kind=="ghes" forces
	// it to true regardless.
	RequireSignedAuthnRequest bool

	// WantAssertionsSigned overrides the default (true). Nil means "use the
	// default"; a non-nil pointer is honored verbatim so an operator can pass
	// --want-assertions-signed=false.
	WantAssertionsSigned *bool

	// ManualACS carries operator-supplied ACS endpoints for the no-metadata
	// path. Ignored when MetadataXML is set (metadata ACS win).
	ManualACS []SPACSEntry
}

// SPACSEntry is the ACS shape carried into the insert transaction. It mirrors
// the package-private acsEntry parsed from metadata.
type SPACSEntry struct {
	Binding   string
	Location  string
	Index     int
	IsDefault bool
}

// BuildSPParams builds the DB insert params for a new SAML SP, the ACS rows,
// and the PEM-encoded signing certs.
//
// If opts.MetadataXML is set it is parsed (with the hardened parseSPMetadata)
// for the entity_id, ACS endpoints, and signing certs. Explicit flags override
// the parsed entity_id/display-name/name-id-format. When no metadata is given,
// opts.ManualACS supplies the ACS endpoints.
//
// Kind=="ghes" installs the GHES default attribute map and forces
// require_signed_authn_request=true. Kind=="generic" (or empty) uses an empty
// attribute map ("[]").
//
// It validates that entity_id is non-empty and at least one ACS endpoint is
// present, returning an error otherwise.
func BuildSPParams(opts SPOptions) (db.InsertSAMLSPParams, []SPACSEntry, []string, error) {
	var (
		entityID = opts.EntityID
		acs      []SPACSEntry
		certPEMs []string
	)

	params := db.InsertSAMLSPParams{}

	if len(opts.MetadataXML) > 0 {
		parsedEntityID, parsedACS, certs, err := parseSPMetadata(opts.MetadataXML)
		if err != nil {
			return db.InsertSAMLSPParams{}, nil, nil, err
		}
		if entityID == "" {
			entityID = parsedEntityID
		}
		for _, e := range parsedACS {
			acs = append(acs, SPACSEntry{
				Binding:   e.Binding,
				Location:  e.Location,
				Index:     e.Index,
				IsDefault: e.IsDefault,
			})
		}
		for _, der := range certs {
			certPEMs = append(certPEMs, string(pem.EncodeToMemory(&pem.Block{
				Type:  "CERTIFICATE",
				Bytes: der,
			})))
		}
		params.MetadataXml = pgtype.Text{String: string(opts.MetadataXML), Valid: true}
	} else {
		acs = append(acs, opts.ManualACS...)
	}

	if entityID == "" {
		return db.InsertSAMLSPParams{}, nil, nil, errors.New("saml-sp: entity-id is required (set --entity-id or supply metadata with an entityID)")
	}
	if len(acs) == 0 {
		return db.InsertSAMLSPParams{}, nil, nil, errors.New("saml-sp: at least one AssertionConsumerService is required (supply metadata with an ACS or --acs-url/--acs-binding)")
	}

	nameIDFormat := persistentNameIDFormat11
	if opts.NameIDFormat != "" {
		nameIDFormat = opts.NameIDFormat
	}

	requireSignedAuthnRequest := opts.RequireSignedAuthnRequest

	switch opts.Kind {
	case "ghes":
		params.SpKind = pgtype.Text{String: "ghes", Valid: true}
		params.AttributeMap = ghesDefaultAttributeMap()
		requireSignedAuthnRequest = true // GHES profile always signs AuthnRequests.
	case "", "generic":
		params.SpKind = pgtype.Text{String: "generic", Valid: true}
		params.AttributeMap = []byte("[]")
	default:
		return db.InsertSAMLSPParams{}, nil, nil, errors.New("saml-sp: unknown --kind " + opts.Kind + " (want ghes or generic)")
	}

	wantAssertionsSigned := true // default per the IdP profile
	if opts.WantAssertionsSigned != nil {
		wantAssertionsSigned = *opts.WantAssertionsSigned
	}

	params.EntityID = entityID
	params.DisplayName = opts.DisplayName
	params.NameIDFormat = nameIDFormat
	params.NameIDClaim = "sub"
	params.WantAssertionsSigned = wantAssertionsSigned
	params.AuthnRequestsSigned = requireSignedAuthnRequest
	params.RequireSignedAuthnRequest = requireSignedAuthnRequest

	return params, acs, certPEMs, nil
}
