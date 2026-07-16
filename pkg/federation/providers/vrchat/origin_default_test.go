//go:build !smoke

package vrchat

import "testing"

func TestDefaultOrigin(t *testing.T) {
	t.Parallel()

	origin, err := resolveOrigin()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := origin.BaseURL.String(), "https://api.vrchat.cloud/api/1"; got != want {
		t.Fatalf("origin = %q, want %q", got, want)
	}
}
