package appaccess

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestParseAndValidateRuleAcceptsNestedRule(t *testing.T) {
	knownProviders := map[string]struct{}{"corporate": {}}
	rule, err := ParseAndValidateRule([]byte(`{
		"version": 1,
		"condition": {
			"op": "all",
			"children": [
				{"fact": "connection.provider", "provider": "corporate"},
				{"op": "any", "children": [
					{"fact": "connection.protocol", "protocol": "oidc"},
					{"op": "not", "child": {"fact": "avatar", "source": "user_uploaded"}}
				]}
			]
		}
	}`), knownProviders)
	if err != nil {
		t.Fatalf("ParseAndValidateRule() error = %v", err)
	}

	if rule.Version != 1 {
		t.Fatalf("Version = %d, want 1", rule.Version)
	}
	if rule.Condition.Op != "all" || len(rule.Condition.Children) != 2 {
		t.Fatalf("root condition = %#v, want all with two children", rule.Condition)
	}
	if got := rule.Condition.Children[0]; got.Fact != "connection.provider" || got.Provider != "corporate" {
		t.Fatalf("provider condition = %#v", got)
	}
	any := rule.Condition.Children[1]
	if any.Op != "any" || len(any.Children) != 2 {
		t.Fatalf("nested condition = %#v, want any with two children", any)
	}
	if got := any.Children[0]; got.Fact != "connection.protocol" || got.Protocol != "oidc" {
		t.Fatalf("protocol condition = %#v", got)
	}
	if any.Children[1].Op != "not" || any.Children[1].Child == nil || any.Children[1].Child.Fact != "avatar" || any.Children[1].Child.Source != "user_uploaded" {
		t.Fatalf("not condition = %#v", any.Children[1])
	}
}

func TestParseAndValidateRuleAcceptsClosedProtocolAndAvatarLiterals(t *testing.T) {
	cases := []string{
		`{"version":1,"condition":{"fact":"connection.protocol","protocol":"oidc"}}`,
		`{"version":1,"condition":{"fact":"connection.protocol","protocol":"steam"}}`,
		`{"version":1,"condition":{"fact":"connection.protocol","protocol":"vrchat"}}`,
		`{"version":1,"condition":{"fact":"avatar","source":"any"}}`,
		`{"version":1,"condition":{"fact":"avatar","source":"user_uploaded"}}`,
	}

	for _, raw := range cases {
		if _, err := ParseAndValidateRule([]byte(raw), nil); err != nil {
			t.Fatalf("ParseAndValidateRule(%s) error = %v", raw, err)
		}
	}
}

func TestParseAndValidateRuleRejectsUnknownAndBounds(t *testing.T) {
	knownProviders := map[string]struct{}{"corporate": {}}
	cases := []struct {
		name   string
		raw    string
		reason string
	}{
		{"unknown field", `{"version":1,"condition":{"fact":"avatar","source":"any","secret":true}}`, "unknown_field"},
		{"empty all", `{"version":1,"condition":{"op":"all","children":[]}}`, "empty_children"},
		{"bad provider", `{"version":1,"condition":{"fact":"connection.provider","provider":"missing"}}`, "provider_not_found"},
		{"bad method", `{"version":1,"condition":{"fact":"login_method","method":"sms"}}`, "invalid_method"},
		{"depth nine", nestedNotRuleJSON(9), "max_depth_exceeded"},
		{"sixty-five nodes", ruleWithNodeCount(65), "max_nodes_exceeded"},
		{"thirty-three children", ruleWithChildren(33), "max_children_exceeded"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAndValidateRule([]byte(tc.raw), knownProviders)
			assertRuleError(t, err, tc.reason)
		})
	}
}

func TestParseAndValidateRuleRejectsClosedSchemaBoundaries(t *testing.T) {
	knownProviders := map[string]struct{}{"corporate": {}}
	cases := []struct {
		name   string
		raw    string
		path   string
		reason string
	}{
		{"missing version", `{"condition":{"fact":"avatar","source":"any"}}`, "$", "missing_version"},
		{"unsupported version", `{"version":2,"condition":{"fact":"avatar","source":"any"}}`, "$", "unsupported_version"},
		{"missing condition", `{"version":1}`, "$", "missing_condition"},
		{"top-level unknown field", `{"version":1,"condition":{"fact":"avatar","source":"any"},"other":true}`, "$", "unknown_field"},
		{"no node shape", `{"version":1,"condition":{}}`, "$.condition", "invalid_shape"},
		{"unknown operator", `{"version":1,"condition":{"op":"xor","children":[{"fact":"avatar","source":"any"}]}}`, "$.condition", "invalid_op"},
		{"all missing children", `{"version":1,"condition":{"op":"all"}}`, "$.condition", "missing_children"},
		{"not missing child", `{"version":1,"condition":{"op":"not"}}`, "$.condition", "missing_child"},
		{"not has children", `{"version":1,"condition":{"op":"not","children":[{"fact":"avatar","source":"any"}]}}`, "$.condition", "invalid_shape"},
		{"combinator and fact", `{"version":1,"condition":{"op":"all","children":[{"fact":"avatar","source":"any"}],"fact":"avatar","source":"any"}}`, "$.condition", "invalid_shape"},
		{"provider missing parameter", `{"version":1,"condition":{"fact":"connection.provider"}}`, "$.condition", "missing_provider"},
		{"provider has unrelated parameter", `{"version":1,"condition":{"fact":"connection.provider","provider":"corporate","source":"any"}}`, "$.condition", "invalid_shape"},
		{"unknown fact", `{"version":1,"condition":{"fact":"other","source":"any"}}`, "$.condition", "invalid_fact"},
		{"invalid protocol", `{"version":1,"condition":{"fact":"connection.protocol","protocol":"saml"}}`, "$.condition", "invalid_protocol"},
		{"invalid source", `{"version":1,"condition":{"fact":"avatar","source":"user"}}`, "$.condition", "invalid_source"},
		{"trailing JSON", `{"version":1,"condition":{"fact":"avatar","source":"any"}} {}`, "$", "trailing_json"},
		{"malformed JSON", `{"version":1,"condition":`, "$", "invalid_json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAndValidateRule([]byte(tc.raw), knownProviders)
			assertRuleErrorAt(t, err, tc.path, tc.reason)
		})
	}
}

func TestParseAndValidateRuleRejectsExplicitNullMembers(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		path   string
		reason string
	}{
		{
			name:   "otherwise-valid leaf has null combinator members",
			raw:    `{"version":1,"condition":{"fact":"avatar","source":"any","op":null,"children":null}}`,
			path:   "$.condition",
			reason: "invalid_shape",
		},
		{
			name:   "otherwise-valid all has null fact member",
			raw:    `{"version":1,"condition":{"op":"all","children":[{"fact":"avatar","source":"any"}],"fact":null}}`,
			path:   "$.condition",
			reason: "invalid_shape",
		},
		{
			name:   "root condition is null",
			raw:    `{"version":1,"condition":null}`,
			path:   "$.condition",
			reason: "invalid_shape",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAndValidateRule([]byte(tc.raw), nil)
			assertRuleErrorAt(t, err, tc.path, tc.reason)
		})
	}
}

func TestRuleErrorMessageIsStable(t *testing.T) {
	err := (&RuleError{Path: "$.condition.children[2]", Reason: "invalid_method"}).Error()
	if want := "invalid group rule at $.condition.children[2]: invalid_method"; err != want {
		t.Fatalf("Error() = %q, want %q", err, want)
	}
}

func assertRuleError(t *testing.T, err error, wantReason string) {
	t.Helper()
	var ruleErr *RuleError
	if !errors.As(err, &ruleErr) {
		t.Fatalf("error = %#v, want *RuleError", err)
	}
	if ruleErr.Reason != wantReason {
		t.Fatalf("Reason = %q, want %q (error = %v)", ruleErr.Reason, wantReason, err)
	}
}

func assertRuleErrorAt(t *testing.T, err error, wantPath, wantReason string) {
	t.Helper()
	assertRuleError(t, err, wantReason)
	var ruleErr *RuleError
	errors.As(err, &ruleErr)
	if ruleErr.Path != wantPath {
		t.Fatalf("Path = %q, want %q (error = %v)", ruleErr.Path, wantPath, err)
	}
}

func nestedNotRuleJSON(depth int) string {
	condition := `{"fact":"avatar","source":"any"}`
	for range depth - 1 {
		condition = `{"op":"not","child":` + condition + `}`
	}
	return `{"version":1,"condition":` + condition + `}`
}

func ruleWithChildren(children int) string {
	leaf := `{"fact":"avatar","source":"any"}`
	return fmt.Sprintf(`{"version":1,"condition":{"op":"all","children":[%s]}}`, strings.TrimSuffix(strings.Repeat(leaf+",", children), ","))
}

func ruleWithNodeCount(nodes int) string {
	if nodes != 65 {
		panic("test helper only defines the required 65-node rule")
	}
	children := strings.TrimSuffix(strings.Repeat(`{"fact":"avatar","source":"any"},`, 31), ",")
	return `{"version":1,"condition":{"op":"all","children":[` +
		`{"op":"all","children":[` + children + `]},` +
		`{"op":"all","children":[` + children + `]}` +
		`]}}`
}
