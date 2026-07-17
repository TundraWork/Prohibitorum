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
	if len(lines) > 2 {
		return nil, errCookiePayloadTooLarge
	}
	totalSize := 0
	for _, line := range lines {
		if line == "" {
			return nil, errInvalidCookie
		}
		if len(line) > maxEncodedCookiesSize-totalSize {
			return nil, errCookiePayloadTooLarge
		}
		totalSize += len(line)
	}
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
		if !normalizeResponseCookie(cookie, now) {
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

func normalizeResponseCookie(cookie *http.Cookie, now time.Time) bool {
	if cookie == nil || (cookie.Name != "auth" && cookie.Name != "twoFactorAuth") ||
		cookie.Path != "/" || !cookie.HttpOnly ||
		len(cookie.Unparsed) != 0 || !validSameSite(cookie.SameSite) || cookie.Valid() != nil {
		return false
	}
	if cookie.Domain != "" && !strings.EqualFold(cookie.Domain, "api.vrchat.cloud") {
		return false
	}
	cookie.Domain = ""
	cookie.Secure = true
	if cookie.MaxAge < 0 {
		cookie.Expires = time.Time{}
		return true
	}
	if cookie.MaxAge > 0 {
		cookie.Expires = now.Add(time.Duration(cookie.MaxAge) * time.Second)
		cookie.MaxAge = 0
	}
	return cookie.Expires.IsZero() || cookie.Expires.After(now)
}

func encodeCookies(cookies []http.Cookie, now time.Time) ([]byte, error) {
	stored := make([]storedCookie, 0, len(cookies))
	seen := make(map[string]struct{}, len(cookies))
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
		clear(encoded)
		return nil, errCookiePayloadTooLarge
	}
	return encoded, nil
}

func decodeCookies(encoded []byte, now time.Time) ([]http.Cookie, error) {
	if len(encoded) > maxEncodedCookiesSize {
		return nil, errCookiePayloadTooLarge
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

func (c *Client) EncodeCookies(cookies []http.Cookie) ([]byte, error) {
	now := time.Now()
	if err := validateOutboundCookies(c.baseURL, cookies, now); err != nil {
		return nil, err
	}
	return encodeCookies(cookies, now)
}

func (c *Client) DecodeCookies(encoded []byte, now time.Time) ([]http.Cookie, error) {
	if !validClientCookieOrigin(c.baseURL) {
		return nil, errInvalidCookieOrigin
	}
	return decodeCookies(encoded, now)
}

func validCookieOrigin(origin *url.URL) bool {
	return validClientCookieOrigin(origin)
}

func validClientCookieOrigin(origin *url.URL) bool {
	if origin == nil {
		return false
	}
	if origin.String() == productionOrigin {
		return true
	}
	return origin.Scheme == "https" && origin.Host != "" && origin.User == nil &&
		origin.RawQuery == "" && !origin.ForceQuery && origin.Fragment == "" &&
		origin.Path == "/api/1" && isLoopbackHost(origin.Hostname())
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validAuthenticationCookie(cookie *http.Cookie, now time.Time) bool {
	if cookie == nil || (cookie.Name != "auth" && cookie.Name != "twoFactorAuth") ||
		cookie.Path != "/" || !cookie.Secure || !cookie.HttpOnly || cookie.MaxAge != 0 ||
		(!cookie.Expires.IsZero() && !cookie.Expires.After(now)) || len(cookie.Unparsed) != 0 || !validSameSite(cookie.SameSite) {
		return false
	}
	return cookie.Valid() == nil
}

func validateOutboundCookies(origin *url.URL, cookies []http.Cookie, now time.Time) error {
	if len(cookies) == 0 {
		return nil
	}
	if !validClientCookieOrigin(origin) {
		return errInvalidCookieOrigin
	}
	totalSize := 0
	seenAuth, seenTwoFactorAuth := false, false
	for i := range cookies {
		cookie := &cookies[i]
		if !validAuthenticationCookie(cookie, now) ||
			(cookie.Domain != "" && !strings.EqualFold(cookie.Domain, origin.Hostname())) {
			return errInvalidCookiePayload
		}
		if cookieNameAlreadySeen(cookie.Name, &seenAuth, &seenTwoFactorAuth) {
			return errInvalidCookiePayload
		}
		size := len(cookie.Name) + 1 + len(cookie.Value)
		if i > 0 {
			size += 2
		}
		if size > maxEncodedCookiesSize-totalSize {
			return errCookiePayloadTooLarge
		}
		totalSize += size
	}
	return nil
}

func mergeCookies(prior, updates []http.Cookie, now time.Time) ([]http.Cookie, error) {
	if err := validateOutboundCookiesForMerge(prior, now); err != nil {
		return nil, err
	}
	merged := append([]http.Cookie(nil), prior...)
	index := make(map[string]int, len(merged))
	for i := range merged {
		merged[i].Domain = ""
		merged[i].MaxAge = 0
		index[merged[i].Name] = i
	}
	for i := range updates {
		update := updates[i]
		if update.MaxAge < 0 {
			if position, ok := index[update.Name]; ok {
				merged = append(merged[:position], merged[position+1:]...)
				index = make(map[string]int, len(merged))
				for j := range merged {
					index[merged[j].Name] = j
				}
			}
			continue
		}
		update.Domain = ""
		update.MaxAge = 0
		if position, ok := index[update.Name]; ok {
			merged[position] = update
		} else {
			index[update.Name] = len(merged)
			merged = append(merged, update)
		}
	}
	if err := validateOutboundCookiesForMerge(merged, now); err != nil {
		return nil, err
	}
	return merged, nil
}

func validateOutboundCookiesForMerge(cookies []http.Cookie, now time.Time) error {
	totalSize := 0
	seenAuth, seenTwoFactorAuth := false, false
	for i := range cookies {
		cookie := &cookies[i]
		if !validAuthenticationCookie(cookie, now) {
			return errInvalidCookiePayload
		}
		if cookieNameAlreadySeen(cookie.Name, &seenAuth, &seenTwoFactorAuth) {
			return errInvalidCookiePayload
		}
		size := len(cookie.Name) + 1 + len(cookie.Value)
		if i > 0 {
			size += 2
		}
		if size > maxEncodedCookiesSize-totalSize {
			return errCookiePayloadTooLarge
		}
		totalSize += size
	}
	return nil
}

func cookieNameAlreadySeen(name string, seenAuth, seenTwoFactorAuth *bool) bool {
	seen := seenAuth
	if name == "twoFactorAuth" {
		seen = seenTwoFactorAuth
	}
	duplicate := *seen
	*seen = true
	return duplicate
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
