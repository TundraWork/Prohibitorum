package saml

import (
	"encoding/json"
	"testing"

	"prohibitorum/pkg/db"
)

// findAttr returns the samlAttr with the given Name and whether it was found.
func findAttr(attrs []samlAttr, name string) (samlAttr, bool) {
	for _, a := range attrs {
		if a.Name == name {
			return a, true
		}
	}
	return samlAttr{}, false
}

func mustAttrJSON(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal attributes: %v", err)
	}
	return b
}

func TestAttributesGHESAdminAccount(t *testing.T) {
	acct := db.Account{
		Username: "alice",
		Role:     "admin",
		Attributes: mustAttrJSON(t, map[string]any{
			"emails":        []any{"a@x.test", "b@x.test"},
			"public_keys":   []any{"ssh-rsa AAAA..."},
			"administrator": true,
		}),
	}

	attrs, err := projectAttributes(acct, ghesDefaultAttributeMap())
	if err != nil {
		t.Fatalf("projectAttributes: %v", err)
	}

	// USERNAME -> single value, basic format.
	username, ok := findAttr(attrs, "USERNAME")
	if !ok {
		t.Fatal("USERNAME attribute missing")
	}
	if len(username.Values) != 1 || username.Values[0] != "alice" {
		t.Errorf("USERNAME values = %v, want [alice]", username.Values)
	}
	if username.NameFormat != nameFormatBasic {
		t.Errorf("USERNAME NameFormat = %q, want %q", username.NameFormat, nameFormatBasic)
	}

	// administrator -> single value "true" for admin.
	admin, ok := findAttr(attrs, "administrator")
	if !ok {
		t.Fatal("administrator attribute missing for admin account")
	}
	if len(admin.Values) != 1 || admin.Values[0] != "true" {
		t.Errorf("administrator values = %v, want [true]", admin.Values)
	}

	// emails -> 2 values, in order.
	emails, ok := findAttr(attrs, "emails")
	if !ok {
		t.Fatal("emails attribute missing")
	}
	if len(emails.Values) != 2 || emails.Values[0] != "a@x.test" || emails.Values[1] != "b@x.test" {
		t.Errorf("emails values = %v, want [a@x.test b@x.test]", emails.Values)
	}

	// public_keys -> uri Name + uri NameFormat, 1 value.
	pk, ok := findAttr(attrs, "urn:oid:1.2.840.113549.1.1.1")
	if !ok {
		t.Fatal("public_keys attribute missing")
	}
	if pk.NameFormat != nameFormatURI {
		t.Errorf("public_keys NameFormat = %q, want %q", pk.NameFormat, nameFormatURI)
	}
	if len(pk.Values) != 1 || pk.Values[0] != "ssh-rsa AAAA..." {
		t.Errorf("public_keys values = %v, want [ssh-rsa AAAA...]", pk.Values)
	}

	// gpg_keys source absent -> omitted.
	if _, ok := findAttr(attrs, "gpg_keys"); ok {
		t.Error("gpg_keys attribute should be omitted (source absent)")
	}

	// Output order matches map order: USERNAME, administrator, emails, public_keys.
	wantOrder := []string{"USERNAME", "administrator", "emails", "urn:oid:1.2.840.113549.1.1.1"}
	if len(attrs) != len(wantOrder) {
		t.Fatalf("got %d attrs %v, want %d", len(attrs), namesOf(attrs), len(wantOrder))
	}
	for i, want := range wantOrder {
		if attrs[i].Name != want {
			t.Errorf("attrs[%d].Name = %q, want %q (order = %v)", i, attrs[i].Name, want, namesOf(attrs))
		}
	}
}

func namesOf(attrs []samlAttr) []string {
	out := make([]string, len(attrs))
	for i, a := range attrs {
		out[i] = a.Name
	}
	return out
}

func TestAttributesNonAdminOmitsAdministrator(t *testing.T) {
	acct := db.Account{
		Username:   "bob",
		Role:       "user",
		Attributes: mustAttrJSON(t, map[string]any{"emails": []any{"bob@x.test"}}),
	}

	attrs, err := projectAttributes(acct, ghesDefaultAttributeMap())
	if err != nil {
		t.Fatalf("projectAttributes: %v", err)
	}

	if _, ok := findAttr(attrs, "administrator"); ok {
		t.Error("administrator attribute should be omitted for non-admin with no administrator attribute")
	}
	// USERNAME and emails should still be present.
	if _, ok := findAttr(attrs, "USERNAME"); !ok {
		t.Error("USERNAME missing")
	}
	if _, ok := findAttr(attrs, "emails"); !ok {
		t.Error("emails missing")
	}
}

func TestAttributesAdministratorTruthyViaAttribute(t *testing.T) {
	// Non-admin role, but administrator attribute is truthy -> emit "true".
	for _, tc := range []struct {
		name string
		val  any
		want bool
	}{
		{"bool true", true, true},
		{"string true", "true", true},
		{"string True", "True", true},
		{"bool false", false, false},
		{"string false", "false", false},
		{"number one", float64(1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acct := db.Account{
				Username:   "carol",
				Role:       "user",
				Attributes: mustAttrJSON(t, map[string]any{"administrator": tc.val}),
			}
			attrs, err := projectAttributes(acct, ghesDefaultAttributeMap())
			if err != nil {
				t.Fatalf("projectAttributes: %v", err)
			}
			admin, ok := findAttr(attrs, "administrator")
			if ok != tc.want {
				t.Errorf("administrator present = %v, want %v", ok, tc.want)
			}
			if ok && (len(admin.Values) != 1 || admin.Values[0] != "true") {
				t.Errorf("administrator values = %v, want [true]", admin.Values)
			}
		})
	}
}

func TestAttributesAdministratorViaRoleOnly(t *testing.T) {
	// Role "admin" with NO administrator key in Attributes exercises the
	// a.Role == "admin" branch of the OR independently. This is the primary
	// production path for the administrator attribute.
	acct := db.Account{
		Username:   "bob",
		Role:       "admin",
		Attributes: mustAttrJSON(t, map[string]any{"emails": []any{"b@x.test"}}),
	}
	attrs, err := projectAttributes(acct, ghesDefaultAttributeMap())
	if err != nil {
		t.Fatalf("projectAttributes: %v", err)
	}
	admin, ok := findAttr(attrs, "administrator")
	if !ok {
		t.Fatal("administrator attribute missing for admin role (no administrator key)")
	}
	if len(admin.Values) != 1 || admin.Values[0] != "true" {
		t.Errorf("administrator values = %v, want [true]", admin.Values)
	}
}

func TestAttributesMalformedAccountAttributes(t *testing.T) {
	// Corrupt a.Attributes JSONB is fail-closed: projectAttributes errors.
	// The map must reference an attributes.* source so the decode path is
	// reached (the decode happens unconditionally before the loop, but use a
	// realistic attributes.* map to make the dependency explicit).
	mapJSON, err := json.Marshal([]attrMapEntry{
		{Name: "emails", NameFormat: nameFormatBasic, Source: "attributes.emails", Multi: true},
	})
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}
	acct := db.Account{Username: "x", Attributes: []byte("{not json")}
	if _, err := projectAttributes(acct, mapJSON); err == nil {
		t.Error("expected error for malformed account Attributes, got nil")
	}
}

func TestAttributesNameFormatURIsExact(t *testing.T) {
	if nameFormatBasic != "urn:oasis:names:tc:SAML:2.0:attrname-format:basic" {
		t.Errorf("nameFormatBasic = %q", nameFormatBasic)
	}
	if nameFormatURI != "urn:oasis:names:tc:SAML:2.0:attrname-format:uri" {
		t.Errorf("nameFormatURI = %q", nameFormatURI)
	}
}

func TestAttributesMalformedMap(t *testing.T) {
	acct := db.Account{Username: "alice"}
	if _, err := projectAttributes(acct, []byte("{not an array}")); err == nil {
		t.Error("expected error for malformed mapJSON, got nil")
	}
}

func TestAttributesEmptyMap(t *testing.T) {
	acct := db.Account{Username: "alice"}
	attrs, err := projectAttributes(acct, nil)
	if err != nil {
		t.Errorf("unexpected error for nil mapJSON: %v", err)
	}
	if len(attrs) != 0 {
		t.Errorf("expected empty slice for nil mapJSON, got %v", namesOf(attrs))
	}
}

func TestAttributesMultiValueOrderAndSingleFromArray(t *testing.T) {
	// Single-value source pointed at an array takes the first element.
	mapJSON, err := json.Marshal([]attrMapEntry{
		{Name: "first_email", NameFormat: nameFormatBasic, Source: "attributes.emails", Multi: false},
	})
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}
	acct := db.Account{
		Username:   "dave",
		Attributes: mustAttrJSON(t, map[string]any{"emails": []any{"first@x.test", "second@x.test"}}),
	}
	attrs, err := projectAttributes(acct, mapJSON)
	if err != nil {
		t.Fatalf("projectAttributes: %v", err)
	}
	a, ok := findAttr(attrs, "first_email")
	if !ok {
		t.Fatal("first_email missing")
	}
	if len(a.Values) != 1 || a.Values[0] != "first@x.test" {
		t.Errorf("first_email values = %v, want [first@x.test]", a.Values)
	}
}
