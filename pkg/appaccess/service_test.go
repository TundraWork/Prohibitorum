package appaccess

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

const passkeyRuleJSON = `{"version":1,"condition":{"fact":"login_method","method":"passkey"}}`
const federationRuleJSON = `{"version":1,"condition":{"fact":"login_method","method":"federation"}}`
const providerRuleJSON = `{"version":1,"condition":{"fact":"connection.provider","provider":"corp"}}`

func TestServiceEvaluateOIDCOpen(t *testing.T) {
	q := &fakeQueries{
		oidcApp: openOIDC("wiki"),
		facts:   accessFacts(42, true),
	}

	got, err := NewService(q).EvaluateOIDC(context.Background(), 42, "wiki")
	if err != nil {
		t.Fatalf("EvaluateOIDC() error = %v", err)
	}
	if !got.Allowed || got.Source != SourceOpen {
		t.Fatalf("EvaluateOIDC() = %#v, want open allow", got)
	}
}

func TestServiceEvaluateOIDCManualDenyPreservesRuleMatches(t *testing.T) {
	q := &fakeQueries{
		oidcApp:     restrictedOIDC("wiki"),
		facts:       accessFacts(42, true),
		manualSet:   true,
		manual:      db.GroupManualDecision{GroupID: 1, GroupKind: "manual", Effect: "deny"},
		manualGroup: manualGroup(1, "reviewed", true),
		oidcGroups:  []db.UserGroup{ruleGroup(2, "passkeys", passkeyRuleJSON, true)},
	}

	got, err := NewService(q).EvaluateOIDC(context.Background(), 42, "wiki")
	if err != nil {
		t.Fatalf("EvaluateOIDC() error = %v", err)
	}
	if got.Allowed || got.Source != SourceManualDeny {
		t.Fatalf("EvaluateOIDC() = %#v, want manual deny", got)
	}
	if len(got.ManualGroups) != 0 {
		t.Fatalf("ManualGroups = %#v, want no claimable groups after deny", got.ManualGroups)
	}
	assertSlugs(t, got.MatchingRuleGroups, "passkeys")
}

func TestServiceEvaluateOIDCManualAllowStillProjectsRuleMatches(t *testing.T) {
	q := &fakeQueries{
		oidcApp:     restrictedOIDC("wiki"),
		facts:       accessFacts(42, true),
		manualSet:   true,
		manual:      db.GroupManualDecision{GroupID: 1, GroupKind: "manual", Effect: "allow"},
		manualGroup: manualGroup(1, "reviewed", true),
		oidcGroups:  []db.UserGroup{ruleGroup(2, "passkeys", passkeyRuleJSON, true)},
	}

	got, err := NewService(q).EvaluateOIDC(context.Background(), 42, "wiki")
	if err != nil || !got.Allowed || got.Source != SourceManualAllow {
		t.Fatalf("EvaluateOIDC() = %#v, %v", got, err)
	}
	assertSlugs(t, got.MatchingRuleGroups, "passkeys")
	if len(got.ManualGroups) != 1 || got.ManualGroups[0].Slug != "reviewed" {
		t.Fatalf("ManualGroups = %#v, want decorated manual group", got.ManualGroups)
	}
	if got, want := got.ExposedGroupSlugs(), []string{"passkeys", "reviewed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ExposedGroupSlugs() = %#v, want %#v", got, want)
	}
}

func TestServiceEvaluateOIDCNeutralRulesUseOR(t *testing.T) {
	q := &fakeQueries{
		oidcApp: restrictedOIDC("wiki"),
		facts:   accessFacts(42, true),
		oidcGroups: []db.UserGroup{
			ruleGroup(1, "federated", federationRuleJSON, true),
			ruleGroup(2, "passkeys", passkeyRuleJSON, false),
		},
	}

	got, err := NewService(q).EvaluateOIDC(context.Background(), 42, "wiki")
	if err != nil {
		t.Fatalf("EvaluateOIDC() error = %v", err)
	}
	if !got.Allowed || got.Source != SourceRule {
		t.Fatalf("EvaluateOIDC() = %#v, want rule allow", got)
	}
	assertSlugs(t, got.MatchingRuleGroups, "passkeys")
}

func TestServiceEvaluateOIDCDisabledKnownProviderRemainsEvaluable(t *testing.T) {
	q := &fakeQueries{
		oidcApp:            restrictedOIDC("wiki"),
		facts:              db.GetAccountAccessFactsRow{ID: 42, ConfirmedProviderSlugs: []string{"corp"}},
		oidcGroups:         []db.UserGroup{ruleGroup(2, "corp", providerRuleJSON, true)},
		knownProviderSlugs: []string{"corp"}, // Includes disabled providers retained in account facts.
	}

	got, err := NewService(q).EvaluateOIDC(context.Background(), 42, "wiki")
	if err != nil {
		t.Fatalf("EvaluateOIDC() error = %v", err)
	}
	if !got.Allowed || got.Source != SourceRule {
		t.Fatalf("EvaluateOIDC() = %#v, want rule allow", got)
	}
}

func TestServiceEvaluateOIDCMissingAppPreservesNoRows(t *testing.T) {
	q := &fakeQueries{oidcErr: pgx.ErrNoRows}

	_, err := NewService(q).EvaluateOIDC(context.Background(), 42, "missing")
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("EvaluateOIDC() error = %v, want pgx.ErrNoRows", err)
	}
}

func TestServiceEvaluateOIDCMalformedPersistedRuleFailsClosed(t *testing.T) {
	q := &fakeQueries{
		oidcApp: restrictedOIDC("wiki"),
		facts:   accessFacts(42, true),
		oidcGroups: []db.UserGroup{
			ruleGroup(2, "broken", `{"version":1,"condition":`, true),
		},
	}

	got, err := NewService(q).EvaluateOIDC(context.Background(), 42, "wiki")
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("EvaluateOIDC() error = %v, want ErrInvalidPolicy", err)
	}
	if got.Allowed {
		t.Fatalf("EvaluateOIDC() = %#v, malformed policy must fail closed", got)
	}
}

func TestServiceListAccountGroupsProjectsCurrentMembership(t *testing.T) {
	q := &fakeQueries{
		facts: accessFacts(42, true),
		globalGroups: []db.UserGroup{
			{ID: 9, Kind: "manual", Slug: "allowed", DisplayName: "Zulu"},
			{ID: 2, Kind: "manual", Slug: "denied", DisplayName: "Denied"},
			{ID: 3, Kind: "manual", Slug: "neutral", DisplayName: "Neutral"},
			{ID: 7, Kind: "rule", Slug: "passkeys-b", DisplayName: "Alpha", Rule: []byte(passkeyRuleJSON)},
			{ID: 6, Kind: "rule", Slug: "passkeys-a", DisplayName: "Alpha", Rule: []byte(passkeyRuleJSON)},
			{ID: 4, Kind: "rule", Slug: "federated", DisplayName: "Federated", Rule: []byte(federationRuleJSON)},
		},
		globalDecisions: []db.GroupManualDecision{
			{GroupID: 9, GroupKind: "manual", AccountID: 42, Effect: "allow"},
			{GroupID: 2, GroupKind: "manual", AccountID: 42, Effect: "deny"},
		},
	}

	groups, err := NewService(q).ListAccountGroups(context.Background(), 42)
	if err != nil {
		t.Fatalf("ListAccountGroups() error = %v", err)
	}
	got := make([]int32, len(groups))
	for i, group := range groups {
		got[i] = group.ID
	}
	if want := []int32{6, 7, 9}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ListAccountGroups() IDs = %v, want %v", got, want)
	}
	if q.factsCalls != 1 {
		t.Fatalf("GetAccountAccessFacts calls = %d, want 1", q.factsCalls)
	}
	if q.knownProviderCalls != 1 {
		t.Fatalf("ListKnownUpstreamIDPSlugs calls = %d, want 1", q.knownProviderCalls)
	}
}

func TestServiceListAccountGroupsDoesNotLoadProvidersForManualGroups(t *testing.T) {
	q := &fakeQueries{
		facts:           accessFacts(42, true),
		globalGroups:    []db.UserGroup{{ID: 1, Kind: "manual", Slug: "members", DisplayName: "Members"}},
		globalDecisions: []db.GroupManualDecision{{GroupID: 1, GroupKind: "manual", AccountID: 42, Effect: "allow"}},
	}

	groups, err := NewService(q).ListAccountGroups(context.Background(), 42)
	if err != nil || len(groups) != 1 || groups[0].ID != 1 {
		t.Fatalf("ListAccountGroups() = %#v, %v", groups, err)
	}
	if q.knownProviderCalls != 0 {
		t.Fatalf("ListKnownUpstreamIDPSlugs calls = %d, want 0", q.knownProviderCalls)
	}
}

func TestServiceListAccountGroupsRejectsInvalidPersistedRule(t *testing.T) {
	q := &fakeQueries{
		facts:        accessFacts(42, true),
		globalGroups: []db.UserGroup{{ID: 2, Kind: "rule", Slug: "broken", Rule: []byte(`{"version":1,"condition":`)}},
	}

	groups, err := NewService(q).ListAccountGroups(context.Background(), 42)
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("ListAccountGroups() error = %v, want ErrInvalidPolicy", err)
	}
	if groups != nil {
		t.Fatalf("ListAccountGroups() = %#v, want nil on invalid policy", groups)
	}
}

func TestServiceEvaluateOIDCUsesOnlyRequestedAppsRuleGroups(t *testing.T) {
	q := &fakeQueries{
		oidcApps: map[string]db.OidcClient{
			"wiki": restrictedOIDC("wiki"),
			"blog": restrictedOIDC("blog"),
		},
		facts: accessFacts(42, true),
		oidcGroupsByClient: map[string][]db.UserGroup{
			"wiki": {ruleGroup(2, "passkeys", passkeyRuleJSON, true)},
			"blog": nil,
		},
	}
	svc := NewService(q)

	wiki, err := svc.EvaluateOIDC(context.Background(), 42, "wiki")
	if err != nil || !wiki.Allowed {
		t.Fatalf("wiki = %#v, %v", wiki, err)
	}
	blog, err := svc.EvaluateOIDC(context.Background(), 42, "blog")
	if err != nil {
		t.Fatalf("blog error = %v", err)
	}
	if blog.Allowed || blog.Source != SourceNoMatch {
		t.Fatalf("blog = %#v, want no-match deny", blog)
	}
}

func TestServiceEvaluateSAML(t *testing.T) {
	q := &fakeQueries{
		samlApp:    db.SamlSp{ID: 9, EntityID: "https://saml.example/sp", AccessRestricted: true},
		facts:      accessFacts(42, true),
		samlGroups: []db.UserGroup{ruleGroup(2, "passkeys", passkeyRuleJSON, true)},
	}

	got, err := NewService(q).EvaluateSAML(context.Background(), 42, 9)
	if err != nil {
		t.Fatalf("EvaluateSAML() error = %v", err)
	}
	if !got.Allowed || got.Source != SourceRule {
		t.Fatalf("EvaluateSAML() = %#v, want rule allow", got)
	}
}

func TestServiceAuthorizeManagerChecksRoleAssignmentAndKind(t *testing.T) {
	q := &fakeQueries{
		oidcApp:     restrictedOIDC("wiki"),
		oidcManaged: true,
		samlManaged: true,
		samlApp:     db.SamlSp{ID: 9},
	}
	svc := NewService(q)
	ctx := context.Background()

	if err := svc.AuthorizeManager(ctx, 7, "admin", AppRef{Kind: "unknown"}); err != nil {
		t.Fatalf("admin AuthorizeManager() error = %v", err)
	}
	if err := svc.AuthorizeManager(ctx, 7, "user", AppRef{Kind: KindOIDC, OIDCClientID: "wiki"}); err != nil {
		t.Fatalf("assigned OIDC manager error = %v", err)
	}
	if err := svc.AuthorizeManager(ctx, 7, "user", AppRef{Kind: KindForwardAuth, OIDCClientID: "wiki"}); !errors.Is(err, ErrAppNotFound) {
		t.Fatalf("wrong OIDC kind error = %v, want ErrAppNotFound", err)
	}
	q.oidcManaged = false
	if err := svc.AuthorizeManager(ctx, 7, "user", AppRef{Kind: KindOIDC, OIDCClientID: "wiki"}); !errors.Is(err, ErrAppNotFound) {
		t.Fatalf("unassigned manager error = %v, want ErrAppNotFound", err)
	}
	q.oidcManaged = true
	if err := svc.AuthorizeManager(ctx, 7, "user", AppRef{Kind: KindSAML, SAMLSPID: 9}); err != nil {
		t.Fatalf("assigned SAML user error = %v", err)
	}
}

func TestServiceListAllowedAppsUsesOneFactSnapshotAndOmitsDenied(t *testing.T) {
	q := &fakeQueries{
		facts: accessFacts(42, true),
		oidcCandidates: []db.ListOIDCAccessCandidatesRow{
			{ClientID: "open", DisplayName: "Open", LaunchUrl: pgtype.Text{String: "https://open.example", Valid: true}, RedirectUris: []string{"https://open.example/callback"}},
		},
		forwardCandidates: []db.ListForwardAuthAccessCandidatesRow{
			{ClientID: "svc", DisplayName: "Service", ForwardAuthHost: pgtype.Text{String: "svc.example", Valid: true}, ForwardAuthScopes: []byte(`[{"name":"read","description":"Read data"}]`), AccessRestricted: true},
		},
		samlCandidates: []db.ListSAMLAccessCandidatesRow{
			{ID: 9, EntityID: "https://saml.example/sp", DisplayName: "SAML", AccessRestricted: true},
		},
		oidcGroupsByClient: map[string][]db.UserGroup{
			"open": nil,
			"svc":  {ruleGroup(2, "passkeys", passkeyRuleJSON, true)},
		},
		samlGroupsByID: map[int64][]db.UserGroup{
			9: {ruleGroup(3, "federated", federationRuleJSON, true)},
		},
	}

	got, err := NewService(q).ListAllowedApps(context.Background(), 42)
	if err != nil {
		t.Fatalf("ListAllowedApps() error = %v", err)
	}
	if q.factsCalls != 1 {
		t.Fatalf("GetAccountAccessFacts calls = %d, want 1", q.factsCalls)
	}
	if len(got) != 2 {
		t.Fatalf("ListAllowedApps() = %#v, want two allowed apps", got)
	}
	if got[0].Ref != (AppRef{Kind: KindOIDC, OIDCClientID: "open"}) || got[0].LaunchURL != "https://open.example" {
		t.Fatalf("OIDC summary = %#v", got[0])
	}
	if got[1].Ref != (AppRef{Kind: KindForwardAuth, OIDCClientID: "svc"}) || got[1].ForwardAuthHost != "svc.example" || !reflect.DeepEqual(got[1].ForwardAuthScopes, []Scope{{Name: "read", Description: "Read data"}}) {
		t.Fatalf("forward-auth summary = %#v", got[1])
	}
}

func TestServicePreviewAndExplainGroupReturnSafeProjection(t *testing.T) {
	q := &fakeQueries{
		oidcApp: restrictedOIDC("wiki"),
		oidcAppGroupsByID: map[string]map[int32]db.UserGroup{
			"wiki": {2: ruleGroup(2, "passkeys", passkeyRuleJSON, true)},
		},
		facts: accessFacts(42, true),
		pageFacts: []db.ListActiveAccountAccessFactsPageRow{
			{ID: 42, Username: "alice", DisplayName: "Alice", HasPasskey: true},
		},
	}
	svc := NewService(q)
	ref := AppRef{Kind: KindOIDC, OIDCClientID: "wiki"}

	preview, err := svc.PreviewGroup(context.Background(), ref, 2, db.ListActiveAccountAccessFactsPageParams{RowLimit: 1})
	if err != nil {
		t.Fatalf("PreviewGroup() error = %v", err)
	}
	wantPreview := []GroupPreview{{Account: AccountSummary{ID: 42, Username: "alice", DisplayName: "Alice"}, Matched: true}}
	if !reflect.DeepEqual(preview, wantPreview) {
		t.Fatalf("PreviewGroup() = %#v, want %#v", preview, wantPreview)
	}
	if q.pageCalls != 1 {
		t.Fatalf("ListActiveAccountAccessFactsPage calls = %d, want 1", q.pageCalls)
	}

	explanation, err := svc.ExplainGroup(context.Background(), ref, 2, 42)
	if err != nil {
		t.Fatalf("ExplainGroup() error = %v", err)
	}
	if !explanation.Result || explanation.Label != "login_method=passkey" {
		t.Fatalf("ExplainGroup() = %#v", explanation)
	}
}

func TestServicePreviewGroupRejectsWrongOIDCAppKind(t *testing.T) {
	q := &fakeQueries{
		oidcApp: db.OidcClient{ClientID: "svc", ForwardAuthEnabled: true},
		oidcAppGroupsByID: map[string]map[int32]db.UserGroup{
			"svc": {2: ruleGroup(2, "passkeys", passkeyRuleJSON, true)},
		},
	}

	_, err := NewService(q).PreviewGroup(
		context.Background(),
		AppRef{Kind: KindOIDC, OIDCClientID: "svc"},
		2,
		db.ListActiveAccountAccessFactsPageParams{RowLimit: 1},
	)
	if !errors.Is(err, ErrAppNotFound) {
		t.Fatalf("PreviewGroup() error = %v, want ErrAppNotFound", err)
	}
}

func TestServicePreviewRuleUsesOneOrderedActiveAccountSnapshot(t *testing.T) {
	q := &fakeQueries{
		oidcApp:            restrictedOIDC("wiki"),
		knownProviderSlugs: []string{"corp"},
		activeFacts: []db.ListActiveAccountAccessFactsRow{
			{ID: 42, Username: "alice", DisplayName: "Alice", HasPasskey: true},
			{ID: 43, Username: "bob", DisplayName: "Bob", HasPasskey: false},
		},
	}

	preview, err := NewService(q).PreviewRule(context.Background(), AppRef{Kind: KindOIDC, OIDCClientID: "wiki"}, Rule{
		Version:   1,
		Condition: Condition{Fact: "login_method", Method: "passkey"},
	})
	if err != nil {
		t.Fatalf("PreviewRule() error = %v", err)
	}
	want := []GroupPreview{
		{Account: AccountSummary{ID: 42, Username: "alice", DisplayName: "Alice"}, Matched: true},
		{Account: AccountSummary{ID: 43, Username: "bob", DisplayName: "Bob"}, Matched: false},
	}
	if !reflect.DeepEqual(preview, want) {
		t.Fatalf("PreviewRule() = %#v, want %#v", preview, want)
	}
	if q.knownProviderCalls != 1 || q.activeFactsCalls != 1 {
		t.Fatalf("PreviewRule queries = providers:%d active facts:%d, want one each", q.knownProviderCalls, q.activeFactsCalls)
	}
	if q.pageCalls != 0 || q.groupCalls != 0 {
		t.Fatalf("PreviewRule loaded persisted policy data: page:%d groups:%d", q.pageCalls, q.groupCalls)
	}
}

func TestServicePreviewRuleValidatesAppRefForEveryKind(t *testing.T) {
	rule := Rule{Version: 1, Condition: Condition{Fact: "login_method", Method: "passkey"}}
	for _, tc := range []struct {
		name string
		ref  AppRef
		q    *fakeQueries
	}{
		{
			name: "oidc",
			ref:  AppRef{Kind: KindOIDC, OIDCClientID: "wiki"},
			q:    &fakeQueries{oidcApp: restrictedOIDC("wiki")},
		},
		{
			name: "forward auth",
			ref:  AppRef{Kind: KindForwardAuth, OIDCClientID: "forward"},
			q:    &fakeQueries{oidcApp: db.OidcClient{ClientID: "forward", ForwardAuthEnabled: true}},
		},
		{
			name: "saml",
			ref:  AppRef{Kind: KindSAML, SAMLSPID: 7},
			q:    &fakeQueries{samlApp: db.SamlSp{ID: 7}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewService(tc.q).PreviewRule(context.Background(), tc.ref, rule); err != nil {
				t.Fatalf("PreviewRule() error = %v", err)
			}
		})
	}

	wrongOIDCKind := &fakeQueries{oidcApp: db.OidcClient{ClientID: "forward", ForwardAuthEnabled: true}}
	if _, err := NewService(wrongOIDCKind).PreviewRule(context.Background(), AppRef{Kind: KindOIDC, OIDCClientID: "forward"}, rule); !errors.Is(err, ErrAppNotFound) {
		t.Fatalf("PreviewRule() error = %v, want ErrAppNotFound", err)
	}
}

func assertSlugs(t *testing.T, groups []GroupMatch, want ...string) {
	t.Helper()
	got := make([]string, 0, len(groups))
	for _, group := range groups {
		got = append(got, group.Slug)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("group slugs = %#v, want %#v", got, want)
	}
}

func openOIDC(clientID string) db.OidcClient {
	return db.OidcClient{ClientID: clientID}
}

func restrictedOIDC(clientID string) db.OidcClient {
	return db.OidcClient{ClientID: clientID, AccessRestricted: true}
}

func accessFacts(accountID int32, passkey bool) db.GetAccountAccessFactsRow {
	return db.GetAccountAccessFactsRow{ID: accountID, HasPasskey: passkey}
}

func manualGroup(id int32, slug string, exposed bool) db.UserGroup {
	return db.UserGroup{ID: id, Kind: "manual", Slug: slug, ExposedToDownstream: exposed}
}

func ruleGroup(id int32, slug, rule string, exposed bool) db.UserGroup {
	return db.UserGroup{ID: id, Kind: "rule", Slug: slug, Rule: []byte(rule), ExposedToDownstream: exposed}
}

type fakeQueries struct {
	oidcApp            db.OidcClient
	oidcApps           map[string]db.OidcClient
	oidcErr            error
	samlApp            db.SamlSp
	samlApps           map[int64]db.SamlSp
	samlErr            error
	facts              db.GetAccountAccessFactsRow
	factsErr           error
	factsCalls         int
	manual             db.GroupManualDecision
	manualSet          bool
	manualErr          error
	manualGroup        db.UserGroup
	oidcGroups         []db.UserGroup
	oidcGroupsByClient map[string][]db.UserGroup
	samlGroups         []db.UserGroup
	samlGroupsByID     map[int64][]db.UserGroup
	oidcAppGroupsByID  map[string]map[int32]db.UserGroup
	samlAppGroupsByID  map[int64]map[int32]db.UserGroup
	knownProviderSlugs []string
	knownProviderCalls int
	globalGroups       []db.UserGroup
	globalDecisions    []db.GroupManualDecision
	groupCalls         int
	oidcManaged        bool
	oidcManagedErr     error
	samlManaged        bool
	samlManagedErr     error
	oidcCandidates     []db.ListOIDCAccessCandidatesRow
	forwardCandidates  []db.ListForwardAuthAccessCandidatesRow
	samlCandidates     []db.ListSAMLAccessCandidatesRow
	pageFacts          []db.ListActiveAccountAccessFactsPageRow
	activeFacts        []db.ListActiveAccountAccessFactsRow
	activeFactsCalls   int
	pageErr            error
	pageCalls          int
}

func (f *fakeQueries) GetOIDCClient(_ context.Context, clientID string) (db.OidcClient, error) {
	return f.oidcClient(clientID)
}

func (f *fakeQueries) GetOIDCClientAny(_ context.Context, clientID string) (db.OidcClient, error) {
	return f.oidcClient(clientID)
}

func (f *fakeQueries) oidcClient(clientID string) (db.OidcClient, error) {
	if f.oidcErr != nil {
		return db.OidcClient{}, f.oidcErr
	}
	if f.oidcApps != nil {
		app, ok := f.oidcApps[clientID]
		if !ok {
			return db.OidcClient{}, pgx.ErrNoRows
		}
		return app, nil
	}
	if f.oidcApp.ClientID != clientID {
		return db.OidcClient{}, pgx.ErrNoRows
	}
	return f.oidcApp, nil
}

func (f *fakeQueries) GetSAMLSPByID(_ context.Context, id int64) (db.SamlSp, error) {
	if f.samlErr != nil {
		return db.SamlSp{}, f.samlErr
	}
	if f.samlApps != nil {
		app, ok := f.samlApps[id]
		if !ok {
			return db.SamlSp{}, pgx.ErrNoRows
		}
		return app, nil
	}
	if f.samlApp.ID != id {
		return db.SamlSp{}, pgx.ErrNoRows
	}
	return f.samlApp, nil
}

func (f *fakeQueries) GetAccountAccessFacts(_ context.Context, _ int32) (db.GetAccountAccessFactsRow, error) {
	f.factsCalls++
	if f.factsErr != nil {
		return db.GetAccountAccessFactsRow{}, f.factsErr
	}
	return f.facts, nil
}

func (f *fakeQueries) ListManualDecisionsForOIDCApp(_ context.Context, _ db.ListManualDecisionsForOIDCAppParams) ([]db.GroupManualDecision, error) {
	return f.manualDecisions()
}

func (f *fakeQueries) ListManualDecisionsForSAMLApp(_ context.Context, _ db.ListManualDecisionsForSAMLAppParams) ([]db.GroupManualDecision, error) {
	return f.manualDecisions()
}

func (f *fakeQueries) manualDecisions() ([]db.GroupManualDecision, error) {
	if f.manualErr != nil {
		return nil, f.manualErr
	}
	if !f.manualSet {
		return nil, nil
	}
	return []db.GroupManualDecision{f.manual}, nil
}

func (f *fakeQueries) GetOIDCAppGroup(_ context.Context, arg db.GetOIDCAppGroupParams) (db.UserGroup, error) {
	f.groupCalls++
	if f.oidcAppGroupsByID != nil {
		groups := f.oidcAppGroupsByID[arg.OidcClientID]
		group, ok := groups[arg.GroupID]
		if !ok {
			return db.UserGroup{}, pgx.ErrNoRows
		}
		return group, nil
	}
	if f.manualGroup.ID == arg.GroupID {
		return f.manualGroup, nil
	}
	return db.UserGroup{}, pgx.ErrNoRows
}

func (f *fakeQueries) GetSAMLAppGroup(_ context.Context, arg db.GetSAMLAppGroupParams) (db.UserGroup, error) {
	f.groupCalls++
	if f.samlAppGroupsByID != nil {
		groups := f.samlAppGroupsByID[arg.SamlSpID]
		group, ok := groups[arg.GroupID]
		if !ok {
			return db.UserGroup{}, pgx.ErrNoRows
		}
		return group, nil
	}
	if f.manualGroup.ID == arg.GroupID {
		return f.manualGroup, nil
	}
	return db.UserGroup{}, pgx.ErrNoRows
}

func (f *fakeQueries) ListOIDCAppRuleGroups(_ context.Context, clientID string) ([]db.UserGroup, error) {
	if f.oidcGroupsByClient != nil {
		return f.oidcGroupsByClient[clientID], nil
	}
	return f.oidcGroups, nil
}

func (f *fakeQueries) ListSAMLAppRuleGroups(_ context.Context, id int64) ([]db.UserGroup, error) {
	if f.samlGroupsByID != nil {
		return f.samlGroupsByID[id], nil
	}
	return f.samlGroups, nil
}

func (f *fakeQueries) ListKnownUpstreamIDPSlugs(_ context.Context) ([]string, error) {
	f.knownProviderCalls++
	return f.knownProviderSlugs, nil
}

func (f *fakeQueries) ListGlobalGroups(context.Context) ([]db.UserGroup, error) {
	return append([]db.UserGroup(nil), f.globalGroups...), nil
}

func (f *fakeQueries) ListGlobalManualDecisionsForAccount(context.Context, int32) ([]db.GroupManualDecision, error) {
	return append([]db.GroupManualDecision(nil), f.globalDecisions...), nil
}

func (f *fakeQueries) IsOIDCClientManager(_ context.Context, _ db.IsOIDCClientManagerParams) (bool, error) {
	return f.oidcManaged, f.oidcManagedErr
}

func (f *fakeQueries) IsSAMLSPManager(_ context.Context, _ db.IsSAMLSPManagerParams) (bool, error) {
	return f.samlManaged, f.samlManagedErr
}

func (f *fakeQueries) ListOIDCAccessCandidates(_ context.Context) ([]db.ListOIDCAccessCandidatesRow, error) {
	return f.oidcCandidates, nil
}

func (f *fakeQueries) ListForwardAuthAccessCandidates(_ context.Context) ([]db.ListForwardAuthAccessCandidatesRow, error) {
	return f.forwardCandidates, nil
}

func (f *fakeQueries) ListSAMLAccessCandidates(_ context.Context) ([]db.ListSAMLAccessCandidatesRow, error) {
	return f.samlCandidates, nil
}

func (f *fakeQueries) ListActiveAccountAccessFactsPage(_ context.Context, _ db.ListActiveAccountAccessFactsPageParams) ([]db.ListActiveAccountAccessFactsPageRow, error) {
	f.pageCalls++
	if f.pageErr != nil {
		return nil, f.pageErr
	}
	return f.pageFacts, nil
}

func (f *fakeQueries) ListActiveAccountAccessFacts(_ context.Context) ([]db.ListActiveAccountAccessFactsRow, error) {
	f.activeFactsCalls++
	return f.activeFacts, nil
}
