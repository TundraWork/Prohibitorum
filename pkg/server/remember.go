package server

import "net/http"

// rememberRequested reads the login page's "keep me signed in" query flag.
// Only the literal "true" counts, so an older client that sends nothing — or
// a hand-written "1" — gets the browser-session default rather than a
// long-lived cookie it did not ask for.
func rememberRequested(r *http.Request) bool {
	return r.URL.Query().Get("remember") == "true"
}
