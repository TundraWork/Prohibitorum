package appaccess

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEvaluateConditionBuildsCompleteNestedExplanation(t *testing.T) {
	condition := Condition{
		Op: "all",
		Children: []Condition{
			{Fact: "connection.provider", Provider: "corporate"},
			{
				Op: "any",
				Children: []Condition{
					{Fact: "connection.protocol", Protocol: "steam"},
					{Fact: "login_method", Method: "passkey"},
				},
			},
			{Op: "not", Child: &Condition{Fact: "avatar", Source: "user_uploaded"}},
		},
	}
	facts := Facts{
		ConfirmedProviders: map[string]struct{}{"corporate": {}},
		ConfirmedProtocols: map[string]struct{}{"oidc": {}},
		HasPasskey:         true,
		HasUserAvatar:      false,
	}

	got := EvaluateCondition(condition, facts)
	want := Explanation{
		Path:   "$",
		Label:  "all",
		Result: true,
		Children: []Explanation{
			{Path: "$.children[0]", Label: "connection.provider=corporate", Result: true},
			{
				Path:   "$.children[1]",
				Label:  "any",
				Result: true,
				Children: []Explanation{
					{Path: "$.children[1].children[0]", Label: "connection.protocol=steam", Result: false},
					{Path: "$.children[1].children[1]", Label: "login_method=passkey", Result: true},
				},
			},
			{
				Path:   "$.children[2]",
				Label:  "not",
				Result: true,
				Children: []Explanation{
					{Path: "$.children[2].child", Label: "avatar=user_uploaded", Result: false},
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EvaluateCondition() = %#v, want %#v", got, want)
	}
}

func TestEvaluateConditionEvaluatesEachFact(t *testing.T) {
	facts := Facts{
		ConfirmedProviders: map[string]struct{}{"corporate": {}},
		ConfirmedProtocols: map[string]struct{}{"oidc": {}, "steam": {}, "vrchat": {}, "saml": {}},
		HasPasskey:         true,
		HasPasswordTOTP:    true,
		HasFederation:      true,
		HasAnyAvatar:       true,
		HasUserAvatar:      true,
	}
	cases := []struct {
		name      string
		condition Condition
		want      bool
	}{
		{"confirmed provider", Condition{Fact: "connection.provider", Provider: "corporate"}, true},
		{"unconfirmed provider", Condition{Fact: "connection.provider", Provider: "missing"}, false},
		{"confirmed oidc protocol", Condition{Fact: "connection.protocol", Protocol: "oidc"}, true},
		{"confirmed steam protocol", Condition{Fact: "connection.protocol", Protocol: "steam"}, true},
		{"confirmed vrchat protocol", Condition{Fact: "connection.protocol", Protocol: "vrchat"}, true},
		{"rejected saml protocol", Condition{Fact: "connection.protocol", Protocol: "saml"}, false},
		{"passkey", Condition{Fact: "login_method", Method: "passkey"}, true},
		{"password totp", Condition{Fact: "login_method", Method: "password_totp"}, true},
		{"federation", Condition{Fact: "login_method", Method: "federation"}, true},
		{"any avatar", Condition{Fact: "avatar", Source: "any"}, true},
		{"user uploaded avatar", Condition{Fact: "avatar", Source: "user_uploaded"}, true},
		{"rejected legacy user avatar", Condition{Fact: "avatar", Source: "user"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateCondition(tc.condition, facts)
			if got.Result != tc.want {
				t.Fatalf("EvaluateCondition(%#v).Result = %t, want %t", tc.condition, got.Result, tc.want)
			}
			if got.Path != "$" || got.Label == "" {
				t.Fatalf("explanation = %#v, want root path and a label", got)
			}
		})
	}
}

func TestExplanationJSONOmitsEmptyChildren(t *testing.T) {
	raw, err := json.Marshal(EvaluateCondition(Condition{Fact: "avatar", Source: "any"}, Facts{HasAnyAvatar: true}))
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	want := `{"path":"$","label":"avatar=any","result":true}`
	if string(raw) != want {
		t.Fatalf("json = %s, want %s", raw, want)
	}
}

func TestDecideManualAndRulePrecedence(t *testing.T) {
	matches := []GroupMatch{{ID: 10, Slug: "passkey", Exposed: true, Matched: true}}
	tests := []struct {
		name       string
		restricted bool
		manual     ManualEffect
		want       bool
		source     DecisionSource
	}{
		{"open ignores policy", false, ManualDeny, true, SourceOpen},
		{"deny wins", true, ManualDeny, false, SourceManualDeny},
		{"allow wins", true, ManualAllow, true, SourceManualAllow},
		{"neutral rule OR", true, ManualNeutral, true, SourceRule},
		{"neutral no match", true, ManualNeutral, false, SourceNoMatch},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inputMatches := matches
			if tc.name == "neutral no match" {
				inputMatches = nil
			}
			got := Decide(tc.restricted, tc.manual, inputMatches)
			if got.Allowed != tc.want || got.Source != tc.source {
				t.Fatalf("Decide() = %#v, want Allowed=%t Source=%q", got, tc.want, tc.source)
			}
		})
	}
}

func TestDecideUsesORAndKeepsEveryMatchingRuleGroup(t *testing.T) {
	matches := []GroupMatch{
		{ID: 1, Slug: "false", Exposed: true, Matched: false},
		{ID: 2, Slug: "first", Exposed: true, Matched: true},
		{ID: 3, Slug: "second", Exposed: false, Matched: true},
	}

	got := Decide(true, ManualNeutral, matches)
	if !got.Allowed || got.Source != SourceRule {
		t.Fatalf("Decide() = %#v, want rule allow", got)
	}
	wantMatches := []GroupMatch{matches[1], matches[2]}
	if !reflect.DeepEqual(got.MatchingRuleGroups, wantMatches) {
		t.Fatalf("MatchingRuleGroups = %#v, want %#v", got.MatchingRuleGroups, wantMatches)
	}
}

func TestDecideManualAllowKeepsRuleMatchesWithoutInferringManualGroup(t *testing.T) {
	matches := []GroupMatch{
		{ID: 1, Slug: "zeta", Exposed: true, Matched: true},
		{ID: 2, Slug: "manual", Exposed: true, Matched: true},
		{ID: 3, Slug: "hidden", Exposed: false, Matched: true},
		{ID: 4, Slug: "ignored", Exposed: true, Matched: false},
	}

	got := Decide(true, ManualAllow, matches)
	if !got.Allowed || got.Source != SourceManualAllow {
		t.Fatalf("Decide() = %#v, want manual allow", got)
	}
	if len(got.ManualGroups) != 0 {
		t.Fatalf("ManualGroups = %#v, want empty because an enum effect has no group identity", got.ManualGroups)
	}
	if want := []GroupMatch{matches[0], matches[1], matches[2]}; !reflect.DeepEqual(got.MatchingRuleGroups, want) {
		t.Fatalf("MatchingRuleGroups = %#v, want %#v", got.MatchingRuleGroups, want)
	}
	if want := []string{"manual", "zeta"}; !reflect.DeepEqual(got.ExposedGroupSlugs(), want) {
		t.Fatalf("ExposedGroupSlugs() = %#v, want %#v", got.ExposedGroupSlugs(), want)
	}
}

func TestDecisionProjectsManualGroupOnlyForManualAllow(t *testing.T) {
	manualGroup := GroupMatch{ID: 7, Slug: "manual", Exposed: true}
	ruleGroup := GroupMatch{ID: 8, Slug: "zeta", Exposed: true, Matched: true}

	allow := Decision{
		Source:             SourceManualAllow,
		ManualGroups:       []GroupMatch{manualGroup},
		MatchingRuleGroups: []GroupMatch{ruleGroup},
	}
	if want := []string{"manual", "zeta"}; !reflect.DeepEqual(allow.ExposedGroupSlugs(), want) {
		t.Fatalf("manual allow ExposedGroupSlugs() = %#v, want %#v", allow.ExposedGroupSlugs(), want)
	}

	deny := Decision{
		Source:             SourceManualDeny,
		ManualGroups:       []GroupMatch{manualGroup},
		MatchingRuleGroups: []GroupMatch{ruleGroup},
	}
	if want := []string{"zeta"}; !reflect.DeepEqual(deny.ExposedGroupSlugs(), want) {
		t.Fatalf("manual deny ExposedGroupSlugs() = %#v, want %#v", deny.ExposedGroupSlugs(), want)
	}
}

func TestDecideGroupsDenyWinsAcrossManualGroups(t *testing.T) {
	manual := []GroupMatch{
		{ID: 1, Slug: "allowed", Exposed: true, Matched: true},
		{ID: 2, Slug: "denied", Exposed: true, Matched: false},
	}
	rules := []GroupMatch{{ID: 3, Slug: "rule", Exposed: true, Matched: true}}
	got := DecideGroups(true, manual, rules)
	if got.Allowed || got.Source != SourceManualDeny {
		t.Fatalf("DecideGroups() = %#v, want manual deny", got)
	}
	if want := []string{"rule"}; !reflect.DeepEqual(got.ExposedGroupSlugs(), want) {
		t.Fatalf("ExposedGroupSlugs() = %#v, want %#v", got.ExposedGroupSlugs(), want)
	}
}

func TestDecideGroupsKeepsAllManualAllows(t *testing.T) {
	manual := []GroupMatch{
		{ID: 1, Slug: "zeta", Exposed: true, Matched: true},
		{ID: 2, Slug: "alpha", Exposed: true, Matched: true},
		{ID: 3, Slug: "hidden", Exposed: false, Matched: true},
	}
	got := DecideGroups(true, manual, nil)
	if !got.Allowed || got.Source != SourceManualAllow {
		t.Fatalf("DecideGroups() = %#v, want manual allow", got)
	}
	if want := []string{"alpha", "zeta"}; !reflect.DeepEqual(got.ExposedGroupSlugs(), want) {
		t.Fatalf("ExposedGroupSlugs() = %#v, want %#v", got.ExposedGroupSlugs(), want)
	}
}
