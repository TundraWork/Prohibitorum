package contract

// SAMLConsentContext is GET /api/prohibitorum/saml-consent — what the advisory
// SAML screen renders. Advisory only: Attributes is informational (no toggles).
type SAMLConsentContext struct {
	SP         SAMLConsentSP `json:"sp"`
	Account    ConsentUser   `json:"account"`
	Attributes []string      `json:"attributes"`
}

type SAMLConsentSP struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	LogoURI     string `json:"logoUri,omitempty"`
}

// SAMLConsentDecision is the POST body. Decision is "approve" or "decline".
type SAMLConsentDecision struct {
	Ticket   string `json:"ticket"`
	Decision string `json:"decision"`
}
