package appaccess

import "sort"

// Facts are the account facts available to pure rule evaluation.
type Facts struct {
	ConfirmedProviders map[string]struct{}
	ConfirmedProtocols map[string]struct{}
	HasPasskey         bool
	HasPasswordTOTP    bool
	HasFederation      bool
	HasAnyAvatar       bool
	HasUserAvatar      bool
}

// Explanation records the result of a condition and each evaluated child.
type Explanation struct {
	Path     string        `json:"path"`
	Label    string        `json:"label"`
	Result   bool          `json:"result"`
	Children []Explanation `json:"children,omitempty"`
}

// ManualEffect is the explicit access effect assigned outside rule evaluation.
type ManualEffect string

const (
	ManualNeutral ManualEffect = "neutral"
	ManualAllow   ManualEffect = "allow"
	ManualDeny    ManualEffect = "deny"
)

// DecisionSource identifies the precedence branch that set a decision.
type DecisionSource string

const (
	SourceOpen        DecisionSource = "open"
	SourceManualDeny  DecisionSource = "manual_deny"
	SourceManualAllow DecisionSource = "manual_allow"
	SourceRule        DecisionSource = "rule"
	SourceNoMatch     DecisionSource = "no_match"
)

// GroupMatch is one group considered during access evaluation.
type GroupMatch struct {
	ID      int32
	Slug    string
	Exposed bool
	Matched bool
}

// Decision is the resolved access result and its claimable groups.
type Decision struct {
	Allowed            bool
	Source             DecisionSource
	ManualGroups       []GroupMatch
	MatchingRuleGroups []GroupMatch
}

// EvaluateCondition evaluates a validated condition and returns its full result tree.
func EvaluateCondition(c Condition, facts Facts) Explanation {
	return evaluateCondition(c, facts, "$")
}

func evaluateCondition(c Condition, facts Facts, path string) Explanation {
	explanation := Explanation{Path: path, Label: conditionLabel(c)}

	switch c.Op {
	case "all":
		explanation.Result = true
		if len(c.Children) > 0 {
			explanation.Children = make([]Explanation, len(c.Children))
		}
		for i, child := range c.Children {
			explanation.Children[i] = evaluateCondition(child, facts, childPath(path, "children", i))
			if !explanation.Children[i].Result {
				explanation.Result = false
			}
		}
	case "any":
		if len(c.Children) > 0 {
			explanation.Children = make([]Explanation, len(c.Children))
		}
		for i, child := range c.Children {
			explanation.Children[i] = evaluateCondition(child, facts, childPath(path, "children", i))
			if explanation.Children[i].Result {
				explanation.Result = true
			}
		}
	case "not":
		if c.Child == nil {
			return explanation
		}
		child := evaluateCondition(*c.Child, facts, path+".child")
		explanation.Children = []Explanation{child}
		explanation.Result = !child.Result
	case "":
		explanation.Result = evaluateFact(c, facts)
	}
	return explanation
}

func evaluateFact(c Condition, facts Facts) bool {
	switch c.Fact {
	case "connection.provider":
		_, found := facts.ConfirmedProviders[c.Provider]
		return found
	case "connection.protocol":
		switch c.Protocol {
		case "oidc", "steam", "vrchat":
			_, found := facts.ConfirmedProtocols[c.Protocol]
			return found
		}
	case "login_method":
		switch c.Method {
		case "passkey":
			return facts.HasPasskey
		case "password_totp":
			return facts.HasPasswordTOTP
		case "federation":
			return facts.HasFederation
		}
	case "avatar":
		switch c.Source {
		case "any":
			return facts.HasAnyAvatar
		case "user_uploaded":
			return facts.HasUserAvatar
		}
	}
	return false
}

func conditionLabel(c Condition) string {
	if c.Op != "" {
		return c.Op
	}
	switch c.Fact {
	case "connection.provider":
		return c.Fact + "=" + c.Provider
	case "connection.protocol":
		return c.Fact + "=" + c.Protocol
	case "login_method":
		return c.Fact + "=" + c.Method
	case "avatar":
		return c.Fact + "=" + c.Source
	default:
		return c.Fact
	}
}

// Decide applies policy activation, manual effects, and matching rules in precedence order.
func Decide(restricted bool, manual ManualEffect, matches []GroupMatch) Decision {
	matchingRuleGroups := matchingGroups(matches)
	decision := Decision{MatchingRuleGroups: matchingRuleGroups}

	if !restricted {
		decision.Allowed = true
		decision.Source = SourceOpen
		return decision
	}

	switch manual {
	case ManualDeny:
		decision.Source = SourceManualDeny
		return decision
	case ManualAllow:
		decision.Allowed = true
		decision.Source = SourceManualAllow
		return decision
	case ManualNeutral:
		if len(matchingRuleGroups) > 0 {
			decision.Allowed = true
			decision.Source = SourceRule
			return decision
		}
		decision.Source = SourceNoMatch
		return decision
	default:
		decision.Source = SourceNoMatch
		return decision
	}
}

// DecideGroups applies the same precedence to every selected manual group. A
// deny in any group wins; otherwise every allow is retained for downstream
// claims and rule matches remain available for both manual and rule decisions.
func DecideGroups(restricted bool, manualGroups, ruleMatches []GroupMatch) Decision {
	decision := Decision{
		ManualGroups:       matchingGroups(manualGroups),
		MatchingRuleGroups: matchingGroups(ruleMatches),
	}
	if !restricted {
		decision.Allowed = true
		decision.Source = SourceOpen
		return decision
	}
	for _, group := range manualGroups {
		if !group.Matched {
			decision.Source = SourceManualDeny
			decision.Allowed = false
			return decision
		}
	}
	if len(decision.ManualGroups) > 0 {
		decision.Allowed = true
		decision.Source = SourceManualAllow
		return decision
	}
	if len(decision.MatchingRuleGroups) > 0 {
		decision.Allowed = true
		decision.Source = SourceRule
		return decision
	}
	decision.Source = SourceNoMatch
	return decision
}

func matchingGroups(matches []GroupMatch) []GroupMatch {
	var matching []GroupMatch
	for _, group := range matches {
		if group.Matched {
			matching = append(matching, group)
		}
	}
	return matching
}

// ExposedGroupSlugs returns sorted, deduplicated exposed groups for claims.
func (d Decision) ExposedGroupSlugs() []string {
	seen := map[string]struct{}{}
	if d.Source == SourceManualAllow {
		for _, group := range d.ManualGroups {
			if group.Exposed {
				seen[group.Slug] = struct{}{}
			}
		}
	}
	for _, group := range d.MatchingRuleGroups {
		if group.Exposed {
			seen[group.Slug] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for slug := range seen {
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}
