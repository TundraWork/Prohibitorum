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

	if !wire.Version.present {
		return Rule{}, ruleError("$", "missing_version")
	}
	if wire.Version.value == nil || *wire.Version.value != 1 {
		return Rule{}, ruleError("$", "unsupported_version")
	}
	if !wire.Condition.present {
		return Rule{}, ruleError("$", "missing_condition")
	}
	if wire.Condition.value == nil {
		return Rule{}, ruleError("$.condition", "invalid_shape")
	}

	nodes := 0
	condition, err := validateCondition(*wire.Condition.value, "$.condition", 1, &nodes, knownProviders)
	if err != nil {
		return Rule{}, err
	}
	return Rule{Version: *wire.Version.value, Condition: condition}, nil
}

type ruleWire struct {
	Version   optionalValue[int]           `json:"version"`
	Condition optionalValue[conditionWire] `json:"condition"`
}

type conditionWire struct {
	Op       optionalValue[string]          `json:"op"`
	Children optionalValue[[]conditionWire] `json:"children"`
	Child    optionalValue[conditionWire]   `json:"child"`
	Fact     optionalValue[string]          `json:"fact"`
	Provider optionalValue[string]          `json:"provider"`
	Protocol optionalValue[string]          `json:"protocol"`
	Method   optionalValue[string]          `json:"method"`
	Source   optionalValue[string]          `json:"source"`
}

type optionalValue[T any] struct {
	present bool
	value   *T
}

func (o *optionalValue[T]) UnmarshalJSON(raw []byte) error {
	o.present = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		o.value = nil
		return nil
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	o.value = &value
	return nil
}

func (o optionalValue[T]) isNull() bool {
	return o.present && o.value == nil
}

func (c *conditionWire) UnmarshalJSON(raw []byte) error {
	type conditionWireAlias conditionWire
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var decoded conditionWireAlias
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*c = conditionWire(decoded)
	return nil
}

func (c conditionWire) hasNull() bool {
	return c.Op.isNull() ||
		c.Children.isNull() ||
		c.Child.isNull() ||
		c.Fact.isNull() ||
		c.Provider.isNull() ||
		c.Protocol.isNull() ||
		c.Method.isNull() ||
		c.Source.isNull()
}

func validateCondition(c conditionWire, path string, depth int, nodes *int, knownProviders map[string]struct{}) (Condition, error) {
	if depth > maxRuleDepth {
		return Condition{}, ruleError(path, "max_depth_exceeded")
	}
	(*nodes)++
	if *nodes > maxRuleNodes {
		return Condition{}, ruleError(path, "max_nodes_exceeded")
	}
	if c.hasNull() {
		return Condition{}, ruleError(path, "invalid_shape")
	}

	hasOp := c.Op.present
	hasFact := c.Fact.present
	if hasOp == hasFact {
		return Condition{}, ruleError(path, "invalid_shape")
	}
	if hasOp {
		return validateCombinator(c, path, depth, nodes, knownProviders)
	}
	return validateFact(c, path, knownProviders)
}

func validateCombinator(c conditionWire, path string, depth int, nodes *int, knownProviders map[string]struct{}) (Condition, error) {
	if c.Fact.present || c.Provider.present || c.Protocol.present || c.Method.present || c.Source.present {
		return Condition{}, ruleError(path, "invalid_shape")
	}

	switch *c.Op.value {
	case "all", "any":
		if c.Child.present {
			return Condition{}, ruleError(path, "invalid_shape")
		}
		if !c.Children.present {
			return Condition{}, ruleError(path, "missing_children")
		}
		if len(*c.Children.value) == 0 {
			return Condition{}, ruleError(path, "empty_children")
		}
		if len(*c.Children.value) > maxRuleChildren {
			return Condition{}, ruleError(path, "max_children_exceeded")
		}

		children := make([]Condition, len(*c.Children.value))
		for i, child := range *c.Children.value {
			var err error
			children[i], err = validateCondition(child, childPath(path, "children", i), depth+1, nodes, knownProviders)
			if err != nil {
				return Condition{}, err
			}
		}
		return Condition{Op: *c.Op.value, Children: children}, nil
	case "not":
		if c.Children.present {
			return Condition{}, ruleError(path, "invalid_shape")
		}
		if !c.Child.present {
			return Condition{}, ruleError(path, "missing_child")
		}
		child, err := validateCondition(*c.Child.value, path+".child", depth+1, nodes, knownProviders)
		if err != nil {
			return Condition{}, err
		}
		return Condition{Op: *c.Op.value, Child: &child}, nil
	default:
		return Condition{}, ruleError(path, "invalid_op")
	}
}

func validateFact(c conditionWire, path string, knownProviders map[string]struct{}) (Condition, error) {
	if c.Children.present || c.Child.present {
		return Condition{}, ruleError(path, "invalid_shape")
	}

	switch *c.Fact.value {
	case "connection.provider":
		if c.Protocol.present || c.Method.present || c.Source.present {
			return Condition{}, ruleError(path, "invalid_shape")
		}
		if !c.Provider.present || *c.Provider.value == "" {
			return Condition{}, ruleError(path, "missing_provider")
		}
		if _, found := knownProviders[*c.Provider.value]; !found {
			return Condition{}, ruleError(path, "provider_not_found")
		}
		return Condition{Fact: *c.Fact.value, Provider: *c.Provider.value}, nil
	case "connection.protocol":
		if c.Provider.present || c.Method.present || c.Source.present {
			return Condition{}, ruleError(path, "invalid_shape")
		}
		if !c.Protocol.present || !validProtocol(*c.Protocol.value) {
			return Condition{}, ruleError(path, "invalid_protocol")
		}
		return Condition{Fact: *c.Fact.value, Protocol: *c.Protocol.value}, nil
	case "login_method":
		if c.Provider.present || c.Protocol.present || c.Source.present {
			return Condition{}, ruleError(path, "invalid_shape")
		}
		if !c.Method.present || !validMethod(*c.Method.value) {
			return Condition{}, ruleError(path, "invalid_method")
		}
		return Condition{Fact: *c.Fact.value, Method: *c.Method.value}, nil
	case "avatar":
		if c.Provider.present || c.Protocol.present || c.Method.present {
			return Condition{}, ruleError(path, "invalid_shape")
		}
		if !c.Source.present || !validSource(*c.Source.value) {
			return Condition{}, ruleError(path, "invalid_source")
		}
		return Condition{Fact: *c.Fact.value, Source: *c.Source.value}, nil
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
