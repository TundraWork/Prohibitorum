package server

import "testing"

func TestResolveOIDCLaunchURL(t *testing.T) {
	cases := []struct {
		name      string
		launch    string
		redirects []string
		want      string
	}{
		{"explicit launch wins", "https://app.example.com/home", []string{"https://app.example.com/cb"}, "https://app.example.com/home"},
		{"trim explicit", "  https://x/y  ", nil, "https://x/y"},
		{"derive origin from first redirect", "", []string{"https://app.example.com/auth/callback"}, "https://app.example.com/"},
		{"skip unparseable, use first valid", "", []string{"not a url", "https://ok.example/cb"}, "https://ok.example/"},
		{"none → empty", "", nil, ""},
		{"redirect without host → empty", "", []string{"/relative/only"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveOIDCLaunchURL(tc.launch, tc.redirects); got != tc.want {
				t.Fatalf("resolveOIDCLaunchURL(%q, %v) = %q, want %q", tc.launch, tc.redirects, got, tc.want)
			}
		})
	}
}
