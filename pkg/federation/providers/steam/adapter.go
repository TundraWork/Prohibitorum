package steam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	federationcore "prohibitorum/pkg/federation"
)

const Protocol = "steam"

type Config struct {
	AllowPrivateNetwork bool `json:"allowPrivateNetwork,omitempty"`
}

type Definition struct{}
func (Definition) Protocol() string { return Protocol }
func (Definition) Descriptor() federationcore.Descriptor {
	return federationcore.Descriptor{Protocol: Protocol, SearchFields: []federationcore.SearchField{{Key: "steamId", Operators: []federationcore.SearchOperator{federationcore.SearchExact}}}, RequiresSecret: true}
}
func (Definition) ValidateConfig(raw json.RawMessage) error {
	var config Config
	if len(raw) == 0 { return errors.New("federation/steam: missing config") }
	if err := json.Unmarshal(raw, &config); err != nil { return fmt.Errorf("federation/steam: decode config: %w", err) }
	return nil
}
func (Definition) ValidateSecret(secret []byte) error {
	if len(secret) == 0 { return errors.New("federation/steam: API key is required") }
	return nil
}
func (d Definition) Ready(provider federationcore.Provider) bool {
	return provider.Protocol == Protocol && !provider.Disabled && provider.Secret != nil && provider.SecretStatus == "valid" && d.ValidateConfig(provider.Config) == nil
}

type Adapter struct {
	secrets *federationcore.SecretStore
	verify func(context.Context, url.Values, string) (string, error)
	summary func(context.Context, string, string) (Summary, error)
}

func NewAdapter(secrets *federationcore.SecretStore) *Adapter { return &Adapter{secrets: secrets} }
func (*Adapter) Protocol() string { return Protocol }

type adapterState struct { ReturnTo string `json:"returnTo"` }

func (a *Adapter) Begin(_ context.Context, _ federationcore.Provider, begin federationcore.BeginContext) (json.RawMessage, federationcore.NextAction, error) {
	callback, err := url.Parse(begin.CallbackURL)
	if err != nil || callback.Scheme == "" || callback.Host == "" { return nil, federationcore.NextAction{}, errors.New("federation/steam: invalid callback URL") }
	query := callback.Query(); query.Set("state", begin.FlowID); callback.RawQuery = query.Encode()
	returnTo := callback.String()
	raw, err := json.Marshal(adapterState{ReturnTo: returnTo}); if err != nil { return nil, federationcore.NextAction{}, err }
	realm := callback.Scheme + "://" + callback.Host
	return raw, federationcore.NextAction{Kind: federationcore.ActionRedirect, URL: BuildAuthURL(realm, returnTo)}, nil
}

func (a *Adapter) Advance(ctx context.Context, provider federationcore.Provider, raw json.RawMessage, input federationcore.ActionInput) (federationcore.AdvanceResult, error) {
	if input.Kind != federationcore.ActionRedirect {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureStateInvalid, nil)
	}
	var state adapterState
	if err := json.Unmarshal(raw, &state); err != nil || state.ReturnTo == "" {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureStateInvalid, nil)
	}
	config, apiKey, err := a.open(provider)
	if err != nil { return federationcore.AdvanceResult{}, err }
	client := federationcore.NewOutboundHTTPClient(config.AllowPrivateNetwork, 2<<20)
	var steamID string
	if a.verify != nil { steamID, err = a.verify(ctx, input.Params, state.ReturnTo) } else { steamID, err = Verify(ctx, client, input.Params, state.ReturnTo) }
	if err != nil {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureSteamVerification, nil)
	}
	var player Summary
	if a.summary != nil { player, err = a.summary(ctx, apiKey, steamID) } else { player, err = FetchSummary(ctx, client, apiKey, steamID) }
	if err != nil {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureSteamVerification, nil)
	}
	avatarURL := player.AvatarURL
	return federationcore.AdvanceResult{Identity: &federationcore.VerifiedIdentity{
		Issuer: Issuer, Subject: steamID, Username: "steam_" + steamID, DisplayName: player.PersonaName,
		EmailVerificationSupported: false, AMR: []string{"steam"}, AvatarURL: avatarURL,
		UpstreamData: map[string]string{
			"steamId": steamID, "personaName": player.PersonaName,
			"profileUrl": "https://steamcommunity.com/profiles/" + steamID, "avatarUrl": avatarURL,
		},
	}}, nil
}

func (a *Adapter) open(provider federationcore.Provider) (Config, string, error) {
	var config Config
	if err := json.Unmarshal(provider.Config, &config); err != nil { return Config{}, "", err }
	if provider.Secret == nil { return Config{}, "", errors.New("federation/steam: provider secret is missing") }
	secret, err := a.secrets.OpenProviderSecret(*provider.Secret, provider.ID)
	if err != nil { return Config{}, "", err }
	if err := (Definition{}).ValidateSecret(secret); err != nil { return Config{}, "", err }
	return config, string(secret), nil
}

var _ federationcore.Definition = Definition{}
var _ federationcore.Adapter = (*Adapter)(nil)
