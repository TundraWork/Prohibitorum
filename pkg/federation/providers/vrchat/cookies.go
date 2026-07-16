package vrchat

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxEncodedCookiesSize = 16 * 1024

var (
	errInvalidCookie         = errors.New("invalid VRChat authentication cookie")
	errInvalidCookiePayload  = errors.New("invalid VRChat cookie payload")
	errCookiePayloadTooLarge = errors.New("VRChat cookie payload exceeds size limit")
	errInvalidCookieOrigin   = errors.New("invalid VRChat cookie origin")
)

type storedCookie struct {
	Name     string        `json:"name"`
	Value    string        `json:"value"`
	Path     string        `json:"path"`
	Expires  time.Time     `json:"expires"`
	Secure   bool          `json:"secure"`
	HTTPOnly bool          `json:"httpOnly"`
	SameSite http.SameSite `json:"sameSite"`
}

func validateResponseCookies(origin *url.URL, header http.Header, now time.Time) ([]http.Cookie, error) {
	if !validCookieOrigin(origin) {
		return nil, errInvalidCookieOrigin
	}

	lines := header.Values("Set-Cookie")
	cookies := make([]http.Cookie, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		if !validSetCookieAttributes(line) {
			return nil, errInvalidCookie
		}

		parsed := (&http.Response{Header: http.Header{"Set-Cookie": []string{line}}}).Cookies()
		if len(parsed) != 1 {
			return nil, errInvalidCookie
		}
		cookie := parsed[0]
		if !validAuthenticationCookie(cookie, now) {
			return nil, errInvalidCookie
		}
		if _, duplicate := seen[cookie.Name]; duplicate {
			return nil, errInvalidCookie
		}
		seen[cookie.Name] = struct{}{}
		cookies = append(cookies, *cookie)
	}
	return cookies, nil
}

func EncodeCookies(cookies []http.Cookie) ([]byte, error) {
	stored := make([]storedCookie, 0, len(cookies))
	seen := make(map[string]struct{}, len(cookies))
	now := time.Now()
	for i := range cookies {
		cookie := &cookies[i]
		if !validAuthenticationCookie(cookie, now) ||
			(cookie.Domain != "" && !strings.EqualFold(cookie.Domain, "api.vrchat.cloud")) {
			return nil, errInvalidCookiePayload
		}
		if _, duplicate := seen[cookie.Name]; duplicate {
			return nil, errInvalidCookiePayload
		}
		seen[cookie.Name] = struct{}{}
		stored = append(stored, storedCookie{
			Name: cookie.Name, Value: cookie.Value, Path: cookie.Path,
			Expires: cookie.Expires, Secure: cookie.Secure,
			HTTPOnly: cookie.HttpOnly, SameSite: cookie.SameSite,
		})
	}

	encoded, err := json.Marshal(stored)
	if err != nil {
		return nil, errInvalidCookiePayload
	}
	if len(encoded) > maxEncodedCookiesSize {
		return nil, errCookiePayloadTooLarge
	}
	return encoded, nil
}

func DecodeCookies(encoded []byte, origin *url.URL, now time.Time) ([]http.Cookie, error) {
	if len(encoded) > maxEncodedCookiesSize {
		return nil, errCookiePayloadTooLarge
	}
	if !validCookieOrigin(origin) {
		return nil, errInvalidCookieOrigin
	}

	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var stored []storedCookie
	if err := decoder.Decode(&stored); err != nil || stored == nil {
		return nil, errInvalidCookiePayload
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errInvalidCookiePayload
	}

	cookies := make([]http.Cookie, 0, len(stored))
	seen := make(map[string]struct{}, len(stored))
	for _, persisted := range stored {
		cookie := http.Cookie{
			Name: persisted.Name, Value: persisted.Value, Path: persisted.Path,
			Expires: persisted.Expires, Secure: persisted.Secure,
			HttpOnly: persisted.HTTPOnly, SameSite: persisted.SameSite,
		}
		if !validAuthenticationCookie(&cookie, now) {
			return nil, errInvalidCookiePayload
		}
		if _, duplicate := seen[cookie.Name]; duplicate {
			return nil, errInvalidCookiePayload
		}
		seen[cookie.Name] = struct{}{}
		cookies = append(cookies, cookie)
	}
	return cookies, nil
}

func validCookieOrigin(origin *url.URL) bool {
	if origin == nil || !strings.EqualFold(origin.Scheme, "https") {
		return false
	}
	host := origin.Hostname()
	if strings.EqualFold(host, "api.vrchat.cloud") || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validAuthenticationCookie(cookie *http.Cookie, now time.Time) bool {
	if cookie == nil || (cookie.Name != "auth" && cookie.Name != "twoFactorAuth") ||
		cookie.Path != "/" || !cookie.Secure || !cookie.HttpOnly || cookie.MaxAge < 0 ||
		(!cookie.Expires.IsZero() && !cookie.Expires.After(now)) || len(cookie.Unparsed) != 0 || !validSameSite(cookie.SameSite) {
		return false
	}
	return cookie.Valid() == nil
}

func validSameSite(sameSite http.SameSite) bool {
	return sameSite >= 0 && sameSite <= http.SameSiteNoneMode
}

func validSetCookieAttributes(line string) bool {
	parts := strings.Split(line, ";")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return false
	}
	name, _, found := strings.Cut(strings.TrimSpace(parts[0]), "=")
	if !found || (name != "auth" && name != "twoFactorAuth") {
		return false
	}

	seen := make(map[string]struct{}, len(parts)-1)
	for _, rawAttribute := range parts[1:] {
		attribute := strings.TrimSpace(rawAttribute)
		if attribute == "" {
			return false
		}
		key, value, hasValue := strings.Cut(attribute, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}

		switch key {
		case "secure", "httponly":
			if hasValue {
				return false
			}
		case "path":
			if !hasValue || value != "/" {
				return false
			}
		case "domain":
			if !hasValue || !strings.EqualFold(value, "api.vrchat.cloud") {
				return false
			}
		case "expires", "max-age":
			if !hasValue || strings.TrimSpace(value) == "" {
				return false
			}
		case "samesite":
			if !hasValue || !(strings.EqualFold(value, "lax") || strings.EqualFold(value, "strict") || strings.EqualFold(value, "none")) {
				return false
			}
		default:
			return false
		}
	}
	return true
}
