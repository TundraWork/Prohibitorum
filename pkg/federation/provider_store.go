package federation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"prohibitorum/pkg/db"
)

var ErrUnknownProvider = errors.New("federation: unknown provider")

type ProviderQueries interface {
	GetUpstreamIDPBySlugAny(context.Context, string) (db.UpstreamIdp, error)
	GetEnrollmentByToken(context.Context, string) (db.Enrollment, error)
}

type ProviderStore struct {
	queries ProviderQueries
	now     func() time.Time
}

func NewProviderStore(queries ProviderQueries) *ProviderStore {
	return &ProviderStore{queries: queries, now: time.Now}
}

func (s *ProviderStore) BySlug(ctx context.Context, slug string) (Provider, error) {
	row, err := s.queries.GetUpstreamIDPBySlugAny(ctx, slug)
	if err != nil {
		// The slug is public input. Collapse absence and database failures to the
		// same opaque classification so begin/link cannot disclose store health
		// or configured-provider membership through their redirect codes.
		return Provider{}, ErrUnknownProvider
	}
	return providerFromRow(row)
}

func (s *ProviderStore) ByBinding(ctx context.Context, id int64, slug, protocol string) (Provider, error) {
	provider, err := s.BySlug(ctx, slug)
	if err != nil {
		return Provider{}, err
	}
	if provider.ID != id || provider.Protocol != protocol {
		return Provider{}, ErrUnknownProvider
	}
	return provider, nil
}

func (s *ProviderStore) InviteProvider(ctx context.Context, token string) (Provider, error) {
	enrollment, err := s.queries.GetEnrollmentByToken(ctx, token)
	if err != nil || enrollment.Intent != "invite" || enrollment.ConsumedAt.Valid || !enrollment.ExpiresAt.Valid || !enrollment.ExpiresAt.Time.After(s.now()) || !enrollment.ExpectedUpstreamIdpSlug.Valid || enrollment.ExpectedUpstreamIdpSlug.String == "" {
		return Provider{}, ErrUnknownProvider
	}
	return s.BySlug(ctx, enrollment.ExpectedUpstreamIdpSlug.String)
}

type providerConfig struct {
	IssuerURL            string   `json:"issuerUrl,omitempty"`
	ClientID             string   `json:"clientId,omitempty"`
	Scopes               []string `json:"scopes,omitempty"`
	AllowedDomains       []string `json:"allowedDomains,omitempty"`
	UsernameClaim        string   `json:"usernameClaim,omitempty"`
	DisplayNameClaim     string   `json:"displayNameClaim,omitempty"`
	EmailClaim           string   `json:"emailClaim,omitempty"`
	PictureClaim         string   `json:"pictureClaim,omitempty"`
	RequireVerifiedEmail bool     `json:"requireVerifiedEmail,omitempty"`
	AllowPrivateNetwork  bool     `json:"allowPrivateNetwork,omitempty"`
}

func providerFromRow(row db.UpstreamIdp) (Provider, error) {
	config, err := json.Marshal(providerConfig{
		IssuerURL: row.IssuerUrl, ClientID: row.ClientID, Scopes: row.Scopes,
		AllowedDomains: row.AllowedDomains, UsernameClaim: row.UsernameClaim,
		DisplayNameClaim: row.DisplayNameClaim, EmailClaim: row.EmailClaim,
		PictureClaim: row.PictureClaim, RequireVerifiedEmail: row.RequireVerifiedEmail,
		AllowPrivateNetwork: row.AllowPrivateNetwork,
	})
	if err != nil {
		return Provider{}, fmt.Errorf("federation: encode provider config: %w", err)
	}
	provider := Provider{
		ID: row.ID, Slug: row.Slug, DisplayName: row.DisplayName, Protocol: row.Protocol,
		Mode: row.Mode, Config: config, Disabled: row.Disabled,
	}
	if len(row.ClientSecretEnc) > 0 || len(row.SecretNonce) > 0 {
		provider.Secret = &SealedSecret{Ciphertext: append([]byte(nil), row.ClientSecretEnc...), Nonce: append([]byte(nil), row.SecretNonce...), KeyVersion: row.KeyVersion}
		provider.SecretStatus = "valid"
	}
	return provider, nil
}

func providerRow(provider Provider) (db.UpstreamIdp, error) {
	var config providerConfig
	if err := json.Unmarshal(provider.Config, &config); err != nil {
		return db.UpstreamIdp{}, fmt.Errorf("federation: decode provider config: %w", err)
	}
	row := db.UpstreamIdp{
		ID: provider.ID, Slug: provider.Slug, DisplayName: provider.DisplayName,
		Protocol: provider.Protocol, Mode: provider.Mode, Disabled: provider.Disabled,
		IssuerUrl: config.IssuerURL, ClientID: config.ClientID, Scopes: append([]string(nil), config.Scopes...),
		AllowedDomains: append([]string(nil), config.AllowedDomains...), UsernameClaim: config.UsernameClaim,
		DisplayNameClaim: config.DisplayNameClaim, EmailClaim: config.EmailClaim, PictureClaim: config.PictureClaim,
		RequireVerifiedEmail: config.RequireVerifiedEmail, AllowPrivateNetwork: config.AllowPrivateNetwork,
	}
	if provider.Secret != nil {
		row.ClientSecretEnc = append([]byte(nil), provider.Secret.Ciphertext...)
		row.SecretNonce = append([]byte(nil), provider.Secret.Nonce...)
		row.KeyVersion = provider.Secret.KeyVersion
	}
	return row, nil
}
