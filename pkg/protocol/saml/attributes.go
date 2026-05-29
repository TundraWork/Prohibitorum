package saml

import (
	"encoding/json"
	"strconv"
	"strings"

	"prohibitorum/pkg/db"
)

// SAML attribute NameFormat URIs. These are the only two formats the GHES
// profile uses; pinned here to guard against namespace drift.
const (
	nameFormatBasic = "urn:oasis:names:tc:SAML:2.0:attrname-format:basic"
	nameFormatURI   = "urn:oasis:names:tc:SAML:2.0:attrname-format:uri"
)

// attrMapEntry is one element of an SP's attribute_map: the ordered JSONB
// array stored in saml_sp.attribute_map (spec/audit R1 shape). Each entry
// declares one SAML attribute to emit and where its value(s) come from on
// the account.
type attrMapEntry struct {
	Name         string `json:"name"`
	NameFormat   string `json:"name_format"`
	FriendlyName string `json:"friendly_name,omitempty"`
	Source       string `json:"source"`
	Multi        bool   `json:"multi"`
}

// samlAttr is the projection output: one resolved SAML attribute ready for
// the assertion builder (Task 7). Attributes that resolve to nothing are
// never emitted, so Values is always non-empty for an emitted attribute.
type samlAttr struct {
	Name         string
	NameFormat   string
	FriendlyName string
	Values       []string
}

// projectAttributes projects an account into SAML attributes per the SP's
// attribute_map (an ordered JSONB array of attrMapEntry). Order is preserved:
// output attributes appear in the same order as the map entries.
//
// Source resolution:
//   - "username"          -> [a.Username]
//   - "attributes.<key>"  -> a.Attributes (JSONB) decoded to map[string]any,
//     then the value at <key>. Multi:true coerces a JSON array to one value
//     per element; Multi:false takes a single scalar (or the first element of
//     an array).
//   - "attributes.administrator" is special: it emits a single value "true"
//     when a.Role == "admin" OR the attribute value is truthy, and is OMITTED
//     entirely otherwise (the GHES profile only ever sends administrator=true).
//
// Any other entry whose source is absent or resolves to empty is omitted
// (no empty <Attribute> is emitted).
//
// A nil/empty mapJSON yields an empty slice and no error. An error is returned
// only when mapJSON is not a valid JSON array.
func projectAttributes(a db.Account, mapJSON []byte) ([]samlAttr, error) {
	if len(mapJSON) == 0 {
		return nil, nil
	}

	var entries []attrMapEntry
	if err := json.Unmarshal(mapJSON, &entries); err != nil {
		return nil, err
	}

	// Decode account attributes once. Nil/empty -> no attributes.
	var attrs map[string]any
	if len(a.Attributes) > 0 {
		if err := json.Unmarshal(a.Attributes, &attrs); err != nil {
			return nil, err
		}
	}

	out := make([]samlAttr, 0, len(entries))
	for _, e := range entries {
		// administrator special rule: only ever emitted as "true".
		if e.Source == "attributes.administrator" {
			if a.Role == "admin" || isTruthy(attrs["administrator"]) {
				out = append(out, samlAttr{
					Name:         e.Name,
					NameFormat:   e.NameFormat,
					FriendlyName: e.FriendlyName,
					Values:       []string{"true"},
				})
			}
			// Not an admin and not truthy -> omit entirely.
			continue
		}

		values := resolveSource(a, attrs, e.Source, e.Multi)
		if len(values) == 0 {
			continue // omit attributes that resolve to nothing
		}
		out = append(out, samlAttr{
			Name:         e.Name,
			NameFormat:   e.NameFormat,
			FriendlyName: e.FriendlyName,
			Values:       values,
		})
	}
	return out, nil
}

// resolveSource resolves a single non-administrator source spec against the
// account into zero or more string values.
func resolveSource(a db.Account, attrs map[string]any, source string, multi bool) []string {
	if source == "username" {
		if a.Username == "" {
			return nil
		}
		return []string{a.Username}
	}

	key, ok := strings.CutPrefix(source, "attributes.")
	if !ok {
		return nil // unknown source kind
	}

	raw, present := attrs[key]
	if !present || raw == nil {
		return nil
	}

	if multi {
		// Coerce a JSON array to one string value per element. A non-array
		// scalar is treated as a single value.
		if arr, isArr := raw.([]any); isArr {
			out := make([]string, 0, len(arr))
			for _, el := range arr {
				if s, ok := toString(el); ok {
					out = append(out, s)
				}
			}
			return out
		}
		if s, ok := toString(raw); ok {
			return []string{s}
		}
		return nil
	}

	// Single-value: a scalar maps to one value; an array source takes just
	// the first stringifiable element.
	if arr, isArr := raw.([]any); isArr {
		for _, el := range arr {
			if s, ok := toString(el); ok {
				return []string{s}
			}
		}
		return nil
	}
	if s, ok := toString(raw); ok {
		return []string{s}
	}
	return nil
}

// isTruthy reports whether a decoded JSON value represents administrator=true.
// Accepts the bool true and the string "true" (case-insensitive); everything
// else (including the JSON number 1, nil, false) is not truthy.
func isTruthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true")
	default:
		return false
	}
}

// toString coerces a scalar decoded-JSON value to a string. Strings pass
// through; bools and numbers are formatted; nil and any non-scalar (maps,
// nested arrays) are not stringifiable. The ok return lets callers skip
// un-stringifiable elements rather than error.
func toString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	case float64:
		// a.Attributes is decoded with plain json.Unmarshal (no UseNumber
		// decoder), so encoding/json decodes every JSON number to float64
		// here. 'f' format avoids scientific notation like "1e+06".
		return strconv.FormatFloat(t, 'f', -1, 64), true
	default:
		return "", false
	}
}

// ghesAttributeMapJSON is the marshaled GHES SAML profile, computed once at
// package init. The entries are a fixed, well-formed literal; marshaling
// cannot fail.
var ghesAttributeMapJSON = func() []byte {
	b, _ := json.Marshal([]attrMapEntry{
		{Name: "USERNAME", NameFormat: nameFormatBasic, Source: "username", Multi: false},
		{Name: "administrator", NameFormat: nameFormatBasic, Source: "attributes.administrator", Multi: false},
		{Name: "emails", NameFormat: nameFormatBasic, Source: "attributes.emails", Multi: true},
		{Name: "urn:oid:1.2.840.113549.1.1.1", NameFormat: nameFormatURI, Source: "attributes.public_keys", Multi: true},
		{Name: "gpg_keys", NameFormat: nameFormatBasic, Source: "attributes.gpg_keys", Multi: true},
	})
	return b
}()

// ghesDefaultAttributeMap returns the GHES SAML profile as a marshaled ordered
// JSONB array, storable directly in saml_sp.attribute_map. The order matters:
// the GHES SP expects USERNAME first, then the optional attributes.
func ghesDefaultAttributeMap() []byte { return ghesAttributeMapJSON }
