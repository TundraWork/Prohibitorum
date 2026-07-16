//go:build !smoke

package vrchat

import "testing"

func TestDefaultOrigin(t *testing.T) {
	t.Parallel()

	origin, err := resolveOrigin()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := origin.BaseURL.String(), productionOrigin; got != want {
		t.Fatalf("origin = %q, want %q", got, want)
	}
}
