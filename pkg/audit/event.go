package audit

import (
	"context"
	"net"
	"net/netip"

	"prohibitorum/pkg/db"
)

type Factor string

const (
	FactorWebAuthn       Factor = "webauthn"
	FactorPassword       Factor = "password"
	FactorTOTP           Factor = "totp"
	FactorRecoveryCode   Factor = "recovery_code"
	FactorFederationOIDC Factor = "federation_oidc"
	FactorEnrollment     Factor = "enrollment"
	FactorSession        Factor = "session"
	FactorOIDCClient     Factor = "oidc_client"
	FactorSAMLSP         Factor = "saml_sp"
)

const (
	EventRegister         = "register"
	EventUse              = "use"
	EventFail             = "fail"
	EventRevoke           = "revoke"
	EventCloneWarning     = "clone_warning"
	EventLink             = "link"
	EventUnlink           = "unlink"
	EventEnrollmentIssued = "enrollment_issued"
	EventSessionStart     = "session_start"
	EventSessionEnd       = "session_end"
)

type Record struct {
	AccountID     *int32
	Factor        Factor
	Event         string
	CredentialRef *int64
	IP            *netip.Addr
	UserAgent     string
	Detail        map[string]any
}

type Writer interface {
	Record(ctx context.Context, r Record) error
}

func NewWriter(q db.Querier) Writer {
	return &dbWriter{q: q}
}

type dbWriter struct{ q db.Querier }

// TODO(v0.2): marshal Detail to JSONB, translate *netip.Addr to pgtype.Inet,
// call q.InsertCredentialEvent.
func (w *dbWriter) Record(ctx context.Context, r Record) error {
	return nil
}

func MustParseIP(s string) *netip.Addr {
	if s == "" {
		return nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		host, _, splitErr := net.SplitHostPort(s)
		if splitErr != nil {
			return nil
		}
		a, err = netip.ParseAddr(host)
		if err != nil {
			return nil
		}
	}
	return &a
}
