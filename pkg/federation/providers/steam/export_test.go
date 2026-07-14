package steam

// SetEndpoints overrides the Steam endpoints for tests (httptest servers). Returns a
// restore func.
func SetEndpoints(login, summary string) func() {
	oldL, oldS := loginEndpoint, summaryEndpoint
	loginEndpoint, summaryEndpoint = login, summary
	return func() { loginEndpoint, summaryEndpoint = oldL, oldS }
}
