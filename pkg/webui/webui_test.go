package webui

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_TemplatesTitle(t *testing.T) {
	h := Handler("Acme SSO")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "<title>Acme SSO</title>") {
		t.Fatalf("title not templated; body head: %.200s", rec.Body.String())
	}
}

func TestHandler_EscapesTitle(t *testing.T) {
	h := Handler(`<x>`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(rec.Body.String(), "<title><x></title>") {
		t.Fatal("instance name was not HTML-escaped")
	}
}

func TestSecurityHeaders_StyleSrcElem(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler("Prohibitorum").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	csp := rec.Header().Get("Content-Security-Policy")

	directives := make(map[string][]string)
	for _, part := range strings.Split(csp, ";") {
		fields := strings.Fields(part)
		if len(fields) > 0 {
			directives[fields[0]] = fields[1:]
		}
	}
	scripts := directives["script-src"]
	if len(scripts) != 1 || scripts[0] != "'self'" {
		t.Errorf("scripts must allow only same-origin resources: %s", csp)
	}
	pressableStyle := "@layer {\n  [data-react-aria-pressable] {\n    touch-action: pan-x pan-y pinch-zoom;\n  }\n}"
	hash := sha256.Sum256([]byte(pressableStyle))
	allowedHash := "'sha256-" + base64.StdEncoding.EncodeToString(hash[:]) + "'"
	styles := directives["style-src-elem"]
	if len(styles) != 2 || styles[0] != "'self'" || styles[1] != allowedHash {
		t.Errorf("styles must allow same-origin resources and the React Aria pressable rule only: %s", csp)
	}
}
