package appaccess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/db"
)

// AppKind identifies the protocol surface of an application.
type AppKind string

const (
	KindOIDC        AppKind = "oidc"
	KindForwardAuth AppKind = "forward_auth"
	KindSAML        AppKind = "saml"
)

// AppRef is the stable identity of one application.
type AppRef struct {
	Kind         AppKind `json:"kind"`
	OIDCClientID string  `json:"oidcClientId,omitempty"`
	SAMLSPID     int64   `json:"samlSpId,omitempty"`
}

// Scope is one forward-auth scope advertised by an application.
type Scope struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AppSummary is the safe application projection returned by the launchpad.
type AppSummary struct {
	Ref                 AppRef  `json:"ref"`
	DisplayName         string  `json:"displayName"`
	LaunchURL           string  `json:"launchUrl,omitempty"`
	RedirectURIs        []string `json:"redirectUris,omitempty"`
	EntityID            string  `json:"entityId,omitempty"`
	ForwardAuthHost     string  `json:"forwardAuthHost,omitempty"`
	ForwardAuthScopes   []Scope `json:"forwardAuthScopes,omitempty"`
	AccessRestricted    bool    `json:"accessRestricted"`
}

// AccountSummary contains only fields safe to return from a rule preview.
type AccountSummary struct {
	ID          int32  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

// GroupPreview reports whether one safe account summary matches a rule group.
type GroupPreview struct {
	Account AccountSummary `json:"account"`
	Matched bool           `json:"matched"`
}

// OIDCAuthorizer authorizes an account for an OIDC or forward-auth app.
type OIDCAuthorizer interface {
	EvaluateOIDC(context.Context, int32, string) (Decision, error)
}

// SAMLAuthorizer authorizes an account for a SAML service provider.
type SAMLAuthorizer interface {
	EvaluateSAML(context.Context, int32, int64) (Decision, error)
}

// AppLister returns launchable applications allowed for an account.
type AppLister interface {
	ListAllowedApps(context.Context, int32) ([]AppSummary, error)
}

var (
	// ErrAppNotFound is deliberately shared by unauthorized managers and invalid app refs.
	ErrAppNotFound = errors.New("appaccess: app not found")
	// ErrInvalidPolicy means persisted app access data cannot be safely evaluated.
	ErrInvalidPolicy = errors.New("appaccess: invalid persisted policy")
)

// queries is intentionally restricted to the generated methods used by Service.
type queries interface {
	GetOIDCClient(context.Context, string) (db.OidcClient, error)
	GetOIDCClientAny(context.Context, string) (db.OidcClient, error)
	GetSAMLSPByID(context.Context, int64) (db.SamlSp, error)
	GetAccountAccessFacts(context.Context, int32) (db.GetAccountAccessFactsRow, error)
	GetManualDecisionForOIDCApp(context.Context, db.GetManualDecisionForOIDCAppParams) (db.GroupManualDecision, error)
	GetManualDecisionForSAMLApp(context.Context, db.GetManualDecisionForSAMLAppParams) (db.GroupManualDecision, error)
	GetOIDCAppGroup(context.Context, db.GetOIDCAppGroupParams) (db.UserGroup, error)
	GetSAMLAppGroup(context.Context, db.GetSAMLAppGroupParams) (db.UserGroup, error)
	ListOIDCAppRuleGroups(context.Context, string) ([]db.UserGroup, error)
	ListSAMLAppRuleGroups(context.Context, int64) ([]db.UserGroup, error)
	ListUpstreamIDPs(context.Context) ([]db.UpstreamIdp, error)
	IsOIDCClientManager(context.Context, db.IsOIDCClientManagerParams) (bool, error)
	IsSAMLSPManager(context.Context, db.IsSAMLSPManagerParams) (bool, error)
	ListOIDCAccessCandidates(context.Context) ([]db.ListOIDCAccessCandidatesRow, error)
	ListForwardAuthAccessCandidates(context.Context) ([]db.ListForwardAuthAccessCandidatesRow, error)
	ListSAMLAccessCandidates(context.Context) ([]db.ListSAMLAccessCandidatesRow, error)
	ListActiveAccountAccessFactsPage(context.Context, db.ListActiveAccountAccessFactsPageParams) ([]db.ListActiveAccountAccessFactsPageRow, error)
}

// Service evaluates live account facts against live app policy.
type Service struct {
	q queries
}

var (
	_ OIDCAuthorizer = (*Service)(nil)
	_ SAMLAuthorizer = (*Service)(nil)
	_ AppLister      = (*Service)(nil)
)

func NewService(q queries) *Service {
	return &Service{q: q}
}

// EvaluateOIDC evaluates access for an enabled OIDC or forward-auth client.
func (s *Service) EvaluateOIDC(ctx context.Context, accountID int32, clientID string) (Decision, error) {
	app, err := s.q.GetOIDCClient(ctx, clientID)
	if err != nil {
		return Decision{}, err
	}
	facts, err := s.loadFacts(ctx, accountID)
	if err != nil {
		return Decision{}, err
	}
	providers, err := s.loadKnownProviders(ctx)
	if err != nil {
		return Decision{}, err
	}
	return s.evaluateOIDCWithFacts(ctx, accountID, app.ClientID, app.AccessRestricted, facts, providers)
}

// EvaluateSAML evaluates access for an enabled SAML service provider.
func (s *Service) EvaluateSAML(ctx context.Context, accountID int32, spID int64) (Decision, error) {
	app, err := s.q.GetSAMLSPByID(ctx, spID)
	if err != nil {
		return Decision{}, err
	}
	if app.Disabled {
		return Decision{}, pgx.ErrNoRows
	}
	facts, err := s.loadFacts(ctx, accountID)
	if err != nil {
		return Decision{}, err
	}
	providers, err := s.loadKnownProviders(ctx)
	if err != nil {
		return Decision{}, err
	}
	return s.evaluateSAMLWithFacts(ctx, accountID, app.ID, app.AccessRestricted, facts, providers)
}

// AuthorizeManager permits global admins, or assigned app managers for the exact app kind.
func (s *Service) AuthorizeManager(ctx context.Context, accountID int32, role string, ref AppRef) error {
	if role == "admin" {
		return nil
	}
	if role != "app_manager" {
		return ErrAppNotFound
	}
	if err := s.validateAppRef(ctx, ref); err != nil {
		return err
	}

	switch ref.Kind {
	case KindOIDC, KindForwardAuth:
		assigned, err := s.q.IsOIDCClientManager(ctx, db.IsOIDCClientManagerParams{
			ClientID:  ref.OIDCClientID,
			AccountID: accountID,
		})
		if err != nil {
			return err
		}
		if !assigned {
			return ErrAppNotFound
		}
		return nil
	case KindSAML:
		assigned, err := s.q.IsSAMLSPManager(ctx, db.IsSAMLSPManagerParams{
			SamlSpID:  ref.SAMLSPID,
			AccountID: accountID,
		})
		if err != nil {
			return err
		}
		if !assigned {
			return ErrAppNotFound
		}
		return nil
	default:
		return ErrAppNotFound
	}
}

// ListAllowedApps returns enabled launchable apps that the account may access.
func (s *Service) ListAllowedApps(ctx context.Context, accountID int32) ([]AppSummary, error) {
	facts, err := s.loadFacts(ctx, accountID)
	if err != nil {
		return nil, err
	}
	providers, err := s.loadKnownProviders(ctx)
	if err != nil {
		return nil, err
	}
	oidcApps, err := s.q.ListOIDCAccessCandidates(ctx)
	if err != nil {
		return nil, err
	}
	forwardAuthApps, err := s.q.ListForwardAuthAccessCandidates(ctx)
	if err != nil {
		return nil, err
	}
	samlApps, err := s.q.ListSAMLAccessCandidates(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]AppSummary, 0, len(oidcApps)+len(forwardAuthApps)+len(samlApps))
	for _, app := range oidcApps {
		decision, err := s.evaluateOIDCWithFacts(ctx, accountID, app.ClientID, app.AccessRestricted, facts, providers)
		if err != nil {
			return nil, err
		}
		if !decision.Allowed {
			continue
		}
		out = append(out, AppSummary{
			Ref:              AppRef{Kind: KindOIDC, OIDCClientID: app.ClientID},
			DisplayName:      app.DisplayName,
			LaunchURL:        app.LaunchUrl.String,
			RedirectURIs:     app.RedirectUris,
			AccessRestricted: app.AccessRestricted,
		})
	}
	for _, app := range forwardAuthApps {
		decision, err := s.evaluateOIDCWithFacts(ctx, accountID, app.ClientID, app.AccessRestricted, facts, providers)
		if err != nil {
			return nil, err
		}
		if !decision.Allowed {
			continue
		}
		scopes, err := parseScopes(app.ForwardAuthScopes)
		if err != nil {
			return nil, err
		}
		out = append(out, AppSummary{
			Ref:               AppRef{Kind: KindForwardAuth, OIDCClientID: app.ClientID},
			DisplayName:       app.DisplayName,
			ForwardAuthHost:   app.ForwardAuthHost.String,
			ForwardAuthScopes: scopes,
			AccessRestricted:  app.AccessRestricted,
		})
	}
	for _, app := range samlApps {
		decision, err := s.evaluateSAMLWithFacts(ctx, accountID, app.ID, app.AccessRestricted, facts, providers)
		if err != nil {
			return nil, err
		}
		if !decision.Allowed {
			continue
		}
		out = append(out, AppSummary{
			Ref:              AppRef{Kind: KindSAML, SAMLSPID: app.ID},
			DisplayName:      app.DisplayName,
			EntityID:         app.EntityID,
			AccessRestricted: app.AccessRestricted,
		})
	}
	return out, nil
}

// PreviewGroup returns only safe account summaries and their match result for one scoped rule group.
func (s *Service) PreviewGroup(ctx context.Context, ref AppRef, groupID int32, page db.ListActiveAccountAccessFactsPageParams) ([]GroupPreview, error) {
	group, err := s.getRuleGroup(ctx, ref, groupID)
	if err != nil {
		return nil, err
	}
	providers, err := s.loadKnownProviders(ctx)
	if err != nil {
		return nil, err
	}
	rule, err := parsePersistedRule(group, providers)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListActiveAccountAccessFactsPage(ctx, page)
	if err != nil {
		return nil, err
	}
	out := make([]GroupPreview, 0, len(rows))
	for _, row := range rows {
		facts, err := factsFromPageRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, GroupPreview{
			Account: AccountSummary{
				ID:          row.ID,
				Username:    row.Username,
				DisplayName: row.DisplayName,
			},
			Matched: EvaluateCondition(rule.Condition, facts).Result,
		})
	}
	return out, nil
}

// ExplainGroup returns the rule evaluator's explanation for one active account.
func (s *Service) ExplainGroup(ctx context.Context, ref AppRef, groupID, accountID int32) (Explanation, error) {
	group, err := s.getRuleGroup(ctx, ref, groupID)
	if err != nil {
		return Explanation{}, err
	}
	providers, err := s.loadKnownProviders(ctx)
	if err != nil {
		return Explanation{}, err
	}
	rule, err := parsePersistedRule(group, providers)
	if err != nil {
		return Explanation{}, err
	}
	facts, err := s.loadFacts(ctx, accountID)
	if err != nil {
		return Explanation{}, err
	}
	return EvaluateCondition(rule.Condition, facts), nil
}

func (s *Service) evaluateOIDCWithFacts(ctx context.Context, accountID int32, clientID string, restricted bool, facts Facts, providers map[string]struct{}) (Decision, error) {
	manual, manualGroup, err := s.loadOIDCManual(ctx, accountID, clientID)
	if err != nil {
		return Decision{}, err
	}
	groups, err := s.q.ListOIDCAppRuleGroups(ctx, clientID)
	if err != nil {
		return Decision{}, err
	}
	return decidePersistedPolicy(restricted, manual, manualGroup, groups, facts, providers)
}

func (s *Service) evaluateSAMLWithFacts(ctx context.Context, accountID int32, spID int64, restricted bool, facts Facts, providers map[string]struct{}) (Decision, error) {
	manual, manualGroup, err := s.loadSAMLManual(ctx, accountID, spID)
	if err != nil {
		return Decision{}, err
	}
	groups, err := s.q.ListSAMLAppRuleGroups(ctx, spID)
	if err != nil {
		return Decision{}, err
	}
	return decidePersistedPolicy(restricted, manual, manualGroup, groups, facts, providers)
}

func (s *Service) loadOIDCManual(ctx context.Context, accountID int32, clientID string) (ManualEffect, *GroupMatch, error) {
	row, err := s.q.GetManualDecisionForOIDCApp(ctx, db.GetManualDecisionForOIDCAppParams{
		OidcClientID: clientID,
		AccountID:    accountID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ManualNeutral, nil, nil
	}
	if err != nil {
		return ManualNeutral, nil, err
	}
	effect, err := manualEffect(row)
	if err != nil {
		return ManualNeutral, nil, err
	}
	group, err := s.q.GetOIDCAppGroup(ctx, db.GetOIDCAppGroupParams{
		GroupID:      row.GroupID,
		OidcClientID: clientID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ManualNeutral, nil, invalidPolicy("manual group %d is missing", row.GroupID)
	}
	if err != nil {
		return ManualNeutral, nil, err
	}
	match, err := manualGroupMatch(group, effect)
	if err != nil {
		return ManualNeutral, nil, err
	}
	return effect, &match, nil
}

func (s *Service) loadSAMLManual(ctx context.Context, accountID int32, spID int64) (ManualEffect, *GroupMatch, error) {
	row, err := s.q.GetManualDecisionForSAMLApp(ctx, db.GetManualDecisionForSAMLAppParams{
		SamlSpID:  spID,
		AccountID: accountID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ManualNeutral, nil, nil
	}
	if err != nil {
		return ManualNeutral, nil, err
	}
	effect, err := manualEffect(row)
	if err != nil {
		return ManualNeutral, nil, err
	}
	group, err := s.q.GetSAMLAppGroup(ctx, db.GetSAMLAppGroupParams{
		GroupID:  row.GroupID,
		SamlSpID: spID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ManualNeutral, nil, invalidPolicy("manual group %d is missing", row.GroupID)
	}
	if err != nil {
		return ManualNeutral, nil, err
	}
	match, err := manualGroupMatch(group, effect)
	if err != nil {
		return ManualNeutral, nil, err
	}
	return effect, &match, nil
}

func (s *Service) loadFacts(ctx context.Context, accountID int32) (Facts, error) {
	row, err := s.q.GetAccountAccessFacts(ctx, accountID)
	if err != nil {
		return Facts{}, err
	}
	return factsFromRow(row)
}

func (s *Service) loadKnownProviders(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.q.ListUpstreamIDPs(ctx)
	if err != nil {
		return nil, err
	}
	providers := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		providers[row.Slug] = struct{}{}
	}
	return providers, nil
}

func (s *Service) getRuleGroup(ctx context.Context, ref AppRef, groupID int32) (db.UserGroup, error) {
	if (ref.Kind == KindOIDC || ref.Kind == KindForwardAuth) && (ref.OIDCClientID == "" || ref.SAMLSPID != 0) {
		return db.UserGroup{}, ErrAppNotFound
	}
	if ref.Kind == KindSAML && (ref.SAMLSPID <= 0 || ref.OIDCClientID != "") {
		return db.UserGroup{}, ErrAppNotFound
	}

	var (
		group db.UserGroup
		err   error
	)
	switch ref.Kind {
	case KindOIDC, KindForwardAuth:
		group, err = s.q.GetOIDCAppGroup(ctx, db.GetOIDCAppGroupParams{
			GroupID:      groupID,
			OidcClientID: ref.OIDCClientID,
		})
	case KindSAML:
		group, err = s.q.GetSAMLAppGroup(ctx, db.GetSAMLAppGroupParams{
			GroupID:  groupID,
			SamlSpID: ref.SAMLSPID,
		})
	default:
		return db.UserGroup{}, ErrAppNotFound
	}
	if err != nil {
		return db.UserGroup{}, err
	}
	if group.Kind != "rule" {
		return db.UserGroup{}, pgx.ErrNoRows
	}
	return group, nil
}

func (s *Service) validateAppRef(ctx context.Context, ref AppRef) error {
	switch ref.Kind {
	case KindOIDC, KindForwardAuth:
		if ref.OIDCClientID == "" || ref.SAMLSPID != 0 {
			return ErrAppNotFound
		}
		app, err := s.q.GetOIDCClientAny(ctx, ref.OIDCClientID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAppNotFound
		}
		if err != nil {
			return err
		}
		if (ref.Kind == KindOIDC && app.ForwardAuthEnabled) || (ref.Kind == KindForwardAuth && !app.ForwardAuthEnabled) {
			return ErrAppNotFound
		}
		return nil
	case KindSAML:
		if ref.SAMLSPID <= 0 || ref.OIDCClientID != "" {
			return ErrAppNotFound
		}
		_, err := s.q.GetSAMLSPByID(ctx, ref.SAMLSPID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAppNotFound
		}
		return err
	default:
		return ErrAppNotFound
	}
}

func decidePersistedPolicy(restricted bool, manual ManualEffect, manualGroup *GroupMatch, groups []db.UserGroup, facts Facts, providers map[string]struct{}) (Decision, error) {
	matches, err := matchRuleGroups(groups, facts, providers)
	if err != nil {
		return Decision{}, err
	}
	decision := Decide(restricted, manual, matches)
	if manualGroup != nil {
		decision.ManualGroup = manualGroup
	}
	return decision, nil
}

func matchRuleGroups(groups []db.UserGroup, facts Facts, providers map[string]struct{}) ([]GroupMatch, error) {
	matches := make([]GroupMatch, 0, len(groups))
	for _, group := range groups {
		rule, err := parsePersistedRule(group, providers)
		if err != nil {
			return nil, err
		}
		explanation := EvaluateCondition(rule.Condition, facts)
		matches = append(matches, GroupMatch{
			ID:      group.ID,
			Slug:    group.Slug,
			Exposed: group.ExposedToDownstream,
			Matched: explanation.Result,
		})
	}
	return matches, nil
}

func parsePersistedRule(group db.UserGroup, providers map[string]struct{}) (Rule, error) {
	if group.Kind != "rule" {
		return Rule{}, invalidPolicy("group %d is not a rule group", group.ID)
	}
	rule, err := ParseAndValidateRule(group.Rule, providers)
	if err != nil {
		return Rule{}, fmt.Errorf("%w: group %d: %w", ErrInvalidPolicy, group.ID, err)
	}
	return rule, nil
}

func manualEffect(row db.GroupManualDecision) (ManualEffect, error) {
	if row.GroupKind != "manual" {
		return ManualNeutral, invalidPolicy("manual decision %d has kind %q", row.GroupID, row.GroupKind)
	}
	switch ManualEffect(row.Effect) {
	case ManualAllow:
		return ManualAllow, nil
	case ManualDeny:
		return ManualDeny, nil
	default:
		return ManualNeutral, invalidPolicy("manual decision %d has effect %q", row.GroupID, row.Effect)
	}
}

func manualGroupMatch(group db.UserGroup, effect ManualEffect) (GroupMatch, error) {
	if group.Kind != "manual" {
		return GroupMatch{}, invalidPolicy("manual decision group %d has kind %q", group.ID, group.Kind)
	}
	return GroupMatch{
		ID:      group.ID,
		Slug:    group.Slug,
		Exposed: group.ExposedToDownstream,
		Matched: effect == ManualAllow,
	}, nil
}

func parseScopes(raw []byte) ([]Scope, error) {
	if len(raw) == 0 {
		return []Scope{}, nil
	}
	var scopes []Scope
	if err := json.Unmarshal(raw, &scopes); err != nil {
		return nil, fmt.Errorf("appaccess: parse forward-auth scopes: %w", err)
	}
	return scopes, nil
}

func invalidPolicy(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidPolicy, fmt.Sprintf(format, args...))
}
