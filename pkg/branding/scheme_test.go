package branding

import (
	"os"
	"regexp"
	"slices"
	"testing"
)

// The dashboard's scheme registry and Schemes must list the same slugs in the
// same order: the server rejects any slug it does not know, and the pickers
// show only the registry's.
func TestSchemesMatchDashboardRegistry(t *testing.T) {
	src, err := os.ReadFile("../../dashboard/src/lib/color-scheme.ts")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range regexp.MustCompile(`(?m)^\s*slug: "([a-z]+)",$`).FindAllSubmatch(src, -1) {
		got = append(got, string(m[1]))
	}
	if !slices.Equal(got, Schemes) {
		t.Fatalf("dashboard registry slugs = %v, want %v", got, Schemes)
	}
	if !ValidScheme(DefaultScheme) {
		t.Fatalf("DefaultScheme %q is not a scheme", DefaultScheme)
	}
}
