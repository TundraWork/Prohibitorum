package appaccess

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
)

const (
	maxRuleDepth    = 8
	maxRuleNodes    = 64
	maxRuleChildren = 32
)

// Rule is the versioned, closed rule document assigned to an application group.
type Rule struct {
	Version   int       `json:"version"`
	Condition Condition `json:"condition"`
}

// Condition is either a combinator node or one fact leaf.
type Condition struct {
	Op       string      `json:"op,omitempty"`
	Children []Condition `json:"children,omitempty"`
	Child    *Condition  `json:"child,omitempty"`
	Fact     string      `json:"fact,omitempty"`
	Provider string      `json:"provider,omitempty"`
	Protocol string      `json:"protocol,omitempty"`
	Method   string      `json:"method,omitempty"`
	Source   string      `json:"source,omitempty"`
}

// RuleError identifies a stable rule-validation failure at a JSON path.
type RuleError struct {
	Path   string
	Reason string
}

func (e *RuleError) Error() string {
	return "invalid group rule at " + e.Path + ": " + e.Reason
}

// ParseAndValidateRule parses one closed rule document and validates its bounded AST.
func ParseAndValidateRule(raw []byte, knownProviders map[string]struct{}) (Rule, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var wire ruleWire
	if err := decoder.Decode(&wire); err != nil {
		return Rule{}, decodeRuleError(err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Rule{}, ruleError("$", "trailing_json")
		}
		return Rule{}, ruleError("$", "invalid_json")
	}

	if wire.Version == nil {
		return Rule{}, ruleError("$", "missing_version")
	}
	if *wire.Version != 1 {
		return Rule{}, ruleError("$", "unsupported_version")
	}
	if wire.Condition == nil {
		return Rule{}, ruleError("$", "missing_condition")
	}

	nodes := 0
	condition, err := validateCondition(*wire.Condition, "$.condition", 1, &nodes, knownProviders)
	if err != nil {
		return Rule{}, err
	}
	return Rule{Version: *wire.Version, Condition: condition}, nil
}

type ruleWire struct {
	Version   *int           `json:"version"`
	Condition *conditionWire `json:"condition"`
}

type conditionWire struct {
	Op       *string          `json:"op"`
	Children *[]conditionWire `json:"children"`
	Child    *conditionWire   `json:"child"`
	Fact     *string          `json:"fact"`
	Provider *string          `json:"provider"`
	Protocol *string          `json:"protocol"`
	Method   *string          `json:"method"`
	Source   *string          `json:"source"`
}

func validateCondition(c conditionWire, path string, depth int, nodes *int, knownProviders map[string]struct{}) (Condition, error) {
	if depth > maxRuleDepth {
		return Condition{}, ruleError(path, "max_depth_exceeded")
	}
	(*nodes)++
	if *nodes > maxRuleNodes {
		return Condition{}, ruleError(path, "max_nodes_exceeded")
	}

	hasOp := c.Op != nil
	hasFact := c.Fact != nil
	if hasOp == hasFact {
		return Condition{}, ruleError(path, "invalid_shape")
	}
	if hasOp {
		return validateCombinator(c, path, depth, nodes, knownProviders)
	}
	return validateFact(c, path, knownProviders)
}

func validateCombinator(c conditionWire, path string, depth int, nodes *int, knownProviders map[string]struct{}) (Condition, error) {
	if c.Fact != nil || c.Provider != nil || c.Protocol != nil || c.Method != nil || c.Source != nil {
		return Condition{}, ruleError(path, "invalid_shape")
	}

	switch *c.Op {
	case "all", "any":
		if c.Child != nil {
			return Condition{}, ruleError(path, "invalid_shape")
		}
		if c.Children == nil {
			return Condition{}, ruleError(path, "missing_children")
		}
		if len(*c.Children) == 0 {
			return Condition{}, ruleError(path, "empty_children")
		}
		if len(*c.Children) > maxRuleChildren {
			return Condition{}, ruleError(path, "max_children_exceeded")
		}

		children := make([]Condition, len(*c.Children))
		for i, child := range *c.Children {
			var err error
			children[i], err = validateCondition(child, childPath(path, "children", i), depth+1, nodes, knownProviders)
			if err != nil {
				return Condition{}, err
			}
		}
		return Condition{Op: *c.Op, Children: children}, nil
	case "not":
		if c.Children != nil {
			return Condition{}, ruleError(path, "invalid_shape")
		}
		if c.Child == nil {
			return Condition{}, ruleError(path, "missing_child")
		}
		child, err := validateCondition(*c.Child, path+".child", depth+1, nodes, knownProviders)
		if err != nil {
			return Condition{}, err
		}
		return Condition{Op: *c.Op, Child: &child}, nil
	default:
		return Condition{}, ruleError(path, "invalid_op")
	}
}

func validateFact(c conditionWire, path string, knownProviders map[string]struct{}) (Condition, error) {
	if c.Children != nil || c.Child != nil {
		return Condition{}, ruleError(path, "invalid_shape")
	}

	switch *c.Fact {
	case "connection.provider":
		if c.Protocol != nil || c.Method != nil || c.Source != nil {
			return Condition{}, ruleError(path, "invalid_shape")
		}
		if c.Provider == nil || *c.Provider == "" {
			return Condition{}, ruleError(path, "missing_provider")
		}
		if _, found := knownProviders[*c.Provider]; !found {
			return Condition{}, ruleError(path, "provider_not_found")
		}
		return Condition{Fact: *c.Fact, Provider: *c.Provider}, nil
	case "connection.protocol":
		if c.Provider != nil || c.Method != nil || c.Source != nil {
			return Condition{}, ruleError(path, "invalid_shape")
		}
		if c.Protocol == nil || !validProtocol(*c.Protocol) {
			return Condition{}, ruleError(path, "invalid_protocol")
		}
		return Condition{Fact: *c.Fact, Protocol: *c.Protocol}, nil
	case "login_method":
		if c.Provider != nil || c.Protocol != nil || c.Source != nil {
			return Condition{}, ruleError(path, "invalid_shape")
		}
		if c.Method == nil || !validMethod(*c.Method) {
			return Condition{}, ruleError(path, "invalid_method")
		}
		return Condition{Fact: *c.Fact, Method: *c.Method}, nil
	case "avatar":
		if c.Provider != nil || c.Protocol != nil || c.Method != nil {
			return Condition{}, ruleError(path, "invalid_shape")
		}
		if c.Source == nil || !validSource(*c.Source) {
			return Condition{}, ruleError(path, "invalid_source")
		}
		return Condition{Fact: *c.Fact, Source: *c.Source}, nil
	default:
		return Condition{}, ruleError(path, "invalid_fact")
	}
}

func validProtocol(protocol string) bool {
	return protocol == "oidc" || protocol == "saml"
}

func validMethod(method string) bool {
	switch method {
	case "passkey", "password_totp", "federation":
		return true
	default:
		return false
	}
}

func validSource(source string) bool {
	return source == "any" || source == "user"
}

func childPath(path, name string, index int) string {
	return path + "." + name + "[" + strconv.Itoa(index) + "]"
}

func decodeRuleError(err error) error {
	if strings.Contains(err.Error(), "unknown field") {
		return ruleError("$", "unknown_field")
	}
	return ruleError("$", "invalid_json")
}

func ruleError(path, reason string) error {
	return &RuleError{Path: path, Reason: reason}
}
