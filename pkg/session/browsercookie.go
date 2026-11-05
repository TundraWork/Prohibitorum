package session

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"prohibitorum/pkg/configx"
)

// TempSessionCookieName holds the entries of accounts signed in without
// "keep me signed in". It carries no Max-Age, so the browser drops it, and
// those accounts with it, when the browser session ends; the remembered
// accounts live in SessionCookieName with Max-Age = remember TTL. One cookie
// can only have one lifetime, hence two.
const TempSessionCookieName = "prohibitorum_session_temp"

// cookieFormatPrefix marks the multi-account value format. A value without it
// is the single-account format from before PHB-285 ("<account_id>.<token>")
// and is still read, so an upgrade signs nobody out.
const cookieFormatPrefix = "2:"

// Entry is one signed-in account in a browser's session cookies.
//
// Seq is the millisecond time the account first signed in on this browser
// (base36 in the cookie). Sorting both cookies' entries by Seq gives the
// fixed sign-in order every account list uses; re-authenticating an account
// keeps its Seq, so neither the order nor the default account moves.
type Entry struct {
	Seq       int64
	AccountID int32
	Token     string
	// Remember says which cookie the entry lives in.
	Remember bool
	// legacy marks an entry read from the old single-account value. Its Seq
	// is unknown (0) until the session is loaded and IssuedAt fills it in.
	legacy bool
}

// PersistentCookieName is the deployment-appropriate name of the cookie for
// remembered accounts (the historical session cookie name).
func PersistentCookieName(cfg *configx.Config) string {
	return SessionCookieNameFor(cfg)
}

// TempCookieName is the deployment-appropriate name of the cookie for
// accounts not remembered.
func TempCookieName(cfg *configx.Config) string {
	if secureCookies(cfg) {
		return "__Host-" + TempSessionCookieName
	}
	return TempSessionCookieName
}

func validCookieToken(tok string) bool {
	if tok == "" {
		return false
	}
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func parseAccountID(s string) (int32, bool) {
	id, err := strconv.ParseInt(s, 10, 32)
	if err != nil || id <= 0 {
		return 0, false
	}
	return int32(id), true
}

// DecodeCookie reads a session cookie value. Entries that do not parse are
// dropped and the rest kept. The old single-account value decodes to one
// legacy entry.
func DecodeCookie(value string) []Entry {
	if !strings.HasPrefix(value, cookieFormatPrefix) {
		dot := strings.IndexByte(value, '.')
		if dot < 1 {
			return nil
		}
		id, ok := parseAccountID(value[:dot])
		if !ok || !validCookieToken(value[dot+1:]) {
			return nil
		}
		return []Entry{{AccountID: id, Token: value[dot+1:], legacy: true}}
	}
	var out []Entry
	for _, raw := range strings.Split(value[len(cookieFormatPrefix):], "~") {
		parts := strings.SplitN(raw, ".", 3)
		if len(parts) != 3 {
			continue
		}
		seq, err := strconv.ParseInt(parts[0], 36, 64)
		if err != nil || seq <= 0 {
			continue
		}
		id, ok := parseAccountID(parts[1])
		if !ok || !validCookieToken(parts[2]) {
			continue
		}
		out = append(out, Entry{Seq: seq, AccountID: id, Token: parts[2]})
	}
	return out
}

// EncodeCookie writes entries in the multi-account format, ordered by Seq.
func EncodeCookie(entries []Entry) string {
	sorted := append([]Entry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })
	var b strings.Builder
	b.WriteString(cookieFormatPrefix)
	for i, e := range sorted {
		if i > 0 {
			b.WriteByte('~')
		}
		b.WriteString(strconv.FormatInt(e.Seq, 36))
		b.WriteByte('.')
		b.WriteString(strconv.FormatInt(int64(e.AccountID), 10))
		b.WriteByte('.')
		b.WriteString(e.Token)
	}
	return b.String()
}

// browserCookie builds one of the two session cookies. Path=/ so the cookie
// reaches the root-level OIDC/SAML protocol endpoints; the name and Secure
// derive from the deployment scheme (see secureCookies). An empty value
// clears the cookie; Name, Path and attributes match the set path or the
// browser would keep the old one.
func browserCookie(cfg *configx.Config, remember bool, value string, rememberTTL time.Duration) *http.Cookie {
	c := &http.Cookie{
		Name:     TempCookieName(cfg),
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookies(cfg),
		SameSite: http.SameSiteLaxMode,
	}
	if remember {
		c.Name = PersistentCookieName(cfg)
		c.MaxAge = int(rememberTTL.Seconds())
	}
	if value == "" {
		c.MaxAge = -1
	}
	return c
}

// WriteBrowserCookies sets both session cookies from entries, splitting them
// by Remember; a cookie left without entries is cleared. Any Set-Cookie for
// either name already in the header is replaced, so a response never carries
// two values for one cookie.
func WriteBrowserCookies(h http.Header, cfg *configx.Config, entries []Entry, rememberTTL time.Duration) {
	writeBrowserCookies(h, cfg, entries, rememberTTL, true, true)
}

func writeBrowserCookies(h http.Header, cfg *configx.Config, entries []Entry, rememberTTL time.Duration, persistent, temp bool) {
	var keep, drop []Entry
	for _, e := range entries {
		if e.Remember {
			keep = append(keep, e)
		} else {
			drop = append(drop, e)
		}
	}
	if persistent {
		setCookieReplacing(h, browserCookie(cfg, true, encodeOrEmpty(keep), rememberTTL))
	}
	if temp {
		setCookieReplacing(h, browserCookie(cfg, false, encodeOrEmpty(drop), rememberTTL))
	}
}

func encodeOrEmpty(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	return EncodeCookie(entries)
}

func setCookieReplacing(h http.Header, c *http.Cookie) {
	prefix := c.Name + "="
	kept := h.Values("Set-Cookie")[:0:0]
	for _, v := range h.Values("Set-Cookie") {
		if !strings.HasPrefix(v, prefix) {
			kept = append(kept, v)
		}
	}
	h.Del("Set-Cookie")
	for _, v := range kept {
		h.Add("Set-Cookie", v)
	}
	if v := c.String(); v != "" {
		h.Add("Set-Cookie", v)
	}
}
