package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyGroupCommandRemoved(t *testing.T) {
	root := buildCLI().Root()
	if _, _, err := root.Find([]string{"group"}); err == nil {
		t.Fatal("legacy group command still registered")
	}
}

func TestPolicyCommandsRegistered(t *testing.T) {
	root := buildCLI().Root()
	operations := [][]string{
		{"manager", "list"},
		{"manager", "assign"},
		{"manager", "remove"},
		{"access", "set-restricted"},
		{"group", "list"},
		{"group", "create-manual"},
		{"group", "create-rule"},
		{"group", "update"},
		{"group", "delete"},
		{"group", "preview"},
		{"decision", "list"},
		{"decision", "set"},
	}
	for _, parent := range []string{"oidc-client", "forward-auth-app", "saml-sp"} {
		for _, operation := range operations {
			path := append([]string{parent}, operation...)
			if _, _, err := root.Find(path); err != nil {
				t.Errorf("command %v not registered: %v", path, err)
			}
		}
	}
}

func TestLegacyAccessFlagsRemoved(t *testing.T) {
	root := buildCLI().Root()
	for _, parent := range []string{"oidc-client", "forward-auth-app", "saml-sp"} {
		cmd, _, err := root.Find([]string{parent, "access"})
		if err != nil {
			t.Fatalf("find %s access: %v", parent, err)
		}
		for _, name := range []string{"access-restricted", "grant-group", "revoke-group", "grant-account", "revoke-account"} {
			if cmd.Flags().Lookup(name) != nil {
				t.Errorf("legacy flag --%s remains on %s access", name, parent)
			}
		}
	}
}

func TestReadCanonicalRuleFileValidatesAndCanonicalizes(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "rule.json")
	raw := `{
		"version": 1,
		"condition": {"fact": "connection.provider", "provider": "corporate"}
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readCanonicalRuleFile(path, map[string]struct{}{"corporate": {}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"condition":{"fact":"connection.provider","provider":"corporate"}}`
	if string(got) != want {
		t.Fatalf("canonical rule = %s, want %s", got, want)
	}
}

func TestReadCanonicalRuleFileRejectsUnknownProviderAndOversize(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	unknown := filepath.Join(dir, "unknown.json")
	if err := os.WriteFile(unknown, []byte(`{"version":1,"condition":{"fact":"connection.provider","provider":"missing"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCanonicalRuleFile(unknown, map[string]struct{}{"corporate": {}}); err == nil || !strings.Contains(err.Error(), "provider_not_found") {
		t.Fatalf("unknown provider error = %v, want provider_not_found", err)
	}

	oversize := filepath.Join(dir, "oversize.json")
	if err := os.WriteFile(oversize, []byte(strings.Repeat("x", maxRuleFileBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCanonicalRuleFile(oversize, nil); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize error = %v, want size rejection", err)
	}
}
