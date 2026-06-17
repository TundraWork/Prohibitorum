package main

import "testing"

func TestIsLoopbackOrigin(t *testing.T) {
	cases := []struct {
		origin string
		want   bool
	}{
		{"http://localhost:8080", true},
		{"https://127.0.0.1", true},
		{"https://[::1]:9000", true},
		{"http://127.0.0.1:18080", true},
		{"https://8.8.8.8", false},       // public IP literal — no DNS needed
		{"https://93.184.216.34", false}, // public IP literal — no DNS needed
		{"not a url", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isLoopbackOrigin(c.origin); got != c.want {
			t.Errorf("isLoopbackOrigin(%q) = %v, want %v", c.origin, got, c.want)
		}
	}
}
