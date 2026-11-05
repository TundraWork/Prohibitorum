package server

import (
	"net/http"
	"strings"

	sessstore "prohibitorum/pkg/session"
)

// isSessionCookie matches either session cookie: remembered accounts live in
// prohibitorum_session, the rest in prohibitorum_session_temp.
func isSessionCookie(c *http.Cookie) bool {
	name := strings.TrimPrefix(c.Name, "__Host-")
	return name == sessstore.SessionCookieName || name == sessstore.TempSessionCookieName
}
