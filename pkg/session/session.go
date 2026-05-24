package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/kv"
)

// SessionStore issues, loads, and revokes sessions backed by pkg/kv.
//
// Key layout:
//
//	session:<account_id>:<random>  → authn.SessionData JSON, TTL = SessionStore.ttl
//
// The account_id prefix lets RevokeAllForAccount enumerate by glob without
// maintaining a secondary index.
type SessionStore struct {
	kv      kv.Store
	ttl     time.Duration
	refresh time.Duration // sliding-refresh threshold; default ttl/4
}

// NewSessionStore constructs a store with the given session TTL.
// Sessions refresh themselves on Load when remaining time < ttl/4.
func NewSessionStore(store kv.Store, ttl time.Duration) *SessionStore {
	return &SessionStore{kv: store, ttl: ttl, refresh: ttl / 4}
}

// TTL returns the configured session lifetime (handler/middleware use it for cookie MaxAge).
func (s *SessionStore) TTL() time.Duration { return s.ttl }

// SudoTTL is the window during which sensitive actions accept the session's
// current sudo grant. Short by design — sudo expires the moment the user
// stops using it.
const SudoTTL = 5 * time.Minute

// newToken produces 32 random bytes encoded as URL-safe base64 (43 chars, no padding).
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("session: rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// newSessionID is a separate opaque id exposed in /me/sessions so listing
// doesn't leak the secret cookie token. 16 bytes is plenty — collisions are
// scoped per-account and would be cosmetic (the revoke endpoint scans by
// account).
func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("session: rand id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func sessionKey(accountID int32, token string) string {
	return fmt.Sprintf("session:%d:%s", accountID, token)
}

// Issue writes a fresh session to KV and returns the random token plus the
// authn.SessionData that was stored. Caller constructs the cookie via CookieValue().
// ua is captured into SessionData so /me/sessions can label each row.
func (s *SessionStore) Issue(ctx context.Context, accountID int32, ip, ua string) (string, *authn.SessionData, error) {
	token, err := newToken()
	if err != nil {
		return "", nil, err
	}
	sessionID, err := newSessionID()
	if err != nil {
		return "", nil, err
	}
	now := time.Now()
	data := &authn.SessionData{
		SessionID:  sessionID,
		AccountID:  accountID,
		IssuedAt:   now,
		ExpiresAt:  now.Add(s.ttl),
		LastSeenIP: ip,
		UserAgent:  ua,
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return "", nil, fmt.Errorf("session: marshal: %w", err)
	}
	if err := s.kv.SetEx(ctx, sessionKey(accountID, token), string(payload), s.ttl); err != nil {
		return "", nil, fmt.Errorf("session: setex: %w", err)
	}
	return token, data, nil
}

// Load returns the session data and a refreshed flag. The refreshed flag is
// true when the TTL was extended on this Load — callers re-emit Set-Cookie
// to update the browser's expiry. Returns ErrNoSession() on missing/expired.
func (s *SessionStore) Load(ctx context.Context, accountID int32, token, ip, ua string) (*authn.SessionData, bool, error) {
	key := sessionKey(accountID, token)
	raw, err := s.kv.Get(ctx, key)
	if err != nil {
		if errors.Is(err, kv.ErrKeyNotFound) {
			return nil, false, authn.ErrNoSession()
		}
		return nil, false, fmt.Errorf("session: get: %w", err)
	}
	var data authn.SessionData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		// Corrupted entry — clean up and report as missing.
		_ = s.kv.Del(ctx, key)
		return nil, false, authn.ErrNoSession()
	}
	now := time.Now()
	if now.After(data.ExpiresAt) {
		_ = s.kv.Del(ctx, key)
		return nil, false, authn.ErrNoSession()
	}
	// Backfill schema fields that landed after the session was originally
	// issued. Pre-existing sessions from before SessionID/UserAgent were
	// added otherwise show empty in /me/sessions and can't be revoked.
	backfilled := false
	if data.SessionID == "" {
		if sid, err := newSessionID(); err == nil {
			data.SessionID = sid
			backfilled = true
		}
	}
	if data.UserAgent == "" && ua != "" {
		data.UserAgent = ua
		backfilled = true
	}
	refreshed := false
	if data.ExpiresAt.Sub(now) < s.refresh {
		data.ExpiresAt = now.Add(s.ttl)
		data.LastSeenIP = ip
		payload, _ := json.Marshal(&data)
		_ = s.kv.SetEx(ctx, key, string(payload), s.ttl)
		refreshed = true
	} else if backfilled {
		// Save the schema fill without resetting the TTL.
		payload, _ := json.Marshal(&data)
		if remaining := time.Until(data.ExpiresAt); remaining > 0 {
			_ = s.kv.SetEx(ctx, key, string(payload), remaining)
		}
	}
	return &data, refreshed, nil
}

// Revoke deletes a specific session.
func (s *SessionStore) Revoke(ctx context.Context, accountID int32, token string) error {
	return s.kv.Del(ctx, sessionKey(accountID, token))
}

// Save writes data back to the same KV entry, preserving the session's
// remaining TTL. Used by the sudo grant handler to stamp SudoUntil onto an
// already-issued session without disturbing its expiry.
func (s *SessionStore) Save(ctx context.Context, accountID int32, token string, data *authn.SessionData) error {
	remaining := time.Until(data.ExpiresAt)
	if remaining <= 0 {
		return authn.ErrNoSession()
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("session: marshal: %w", err)
	}
	if err := s.kv.SetEx(ctx, sessionKey(accountID, token), string(payload), remaining); err != nil {
		return fmt.Errorf("session: setex: %w", err)
	}
	return nil
}

// SessionRecord carries a live session for accountID. Each entry is the
// stored authn.SessionData with its KV-suffix token attached (callers need it
// only to compute is-current; the token MUST NOT be echoed to clients).
type SessionRecord struct {
	Token string // raw cookie-half; do not echo
	Data  authn.SessionData
}

func (s *SessionStore) ListByAccount(ctx context.Context, accountID int32) ([]SessionRecord, error) {
	pattern := fmt.Sprintf("session:%d:*", accountID)
	prefix := fmt.Sprintf("session:%d:", accountID)
	var (
		cursor uint64
		out    []SessionRecord
	)
	for {
		result, err := s.kv.ScanEntries(ctx, pattern, cursor, 100)
		if err != nil {
			return nil, fmt.Errorf("session: scan: %w", err)
		}
		for _, entry := range result.Entries {
			var data authn.SessionData
			if err := json.Unmarshal([]byte(entry.Value), &data); err != nil {
				continue
			}
			token := strings.TrimPrefix(entry.Key, prefix)
			// Backfill SessionID for pre-schema sessions so the revoke
			// endpoint can target them. UA stays empty until the session
			// is used again (Load backfills it from the request UA).
			if data.SessionID == "" {
				if sid, err := newSessionID(); err == nil {
					data.SessionID = sid
					if remaining := time.Until(data.ExpiresAt); remaining > 0 {
						if payload, err := json.Marshal(&data); err == nil {
							_ = s.kv.SetEx(ctx, entry.Key, string(payload), remaining)
						}
					}
				}
			}
			out = append(out, SessionRecord{Token: token, Data: data})
		}
		cursor = result.NextCursor
		if cursor == 0 {
			break
		}
	}
	return out, nil
}

// RevokeBySessionID drops a single session belonging to accountID whose
// SessionData.SessionID matches the given id. Returns true when a match
// was found and deleted, false otherwise (e.g. unknown id, race with
// expiry, wrong account). Refusing to surface "not yours" vs "not found"
// distinctly here protects against probe attacks across accounts.
func (s *SessionStore) RevokeBySessionID(ctx context.Context, accountID int32, sessionID string) (bool, error) {
	sessions, err := s.ListByAccount(ctx, accountID)
	if err != nil {
		return false, err
	}
	for _, sr := range sessions {
		if sr.Data.SessionID == sessionID {
			if err := s.kv.Del(ctx, sessionKey(accountID, sr.Token)); err != nil {
				return false, fmt.Errorf("session: del: %w", err)
			}
			return true, nil
		}
	}
	return false, nil
}

// RevokeAllForAccount removes every session for the given account. Returns the
// count of deleted entries and the first error encountered (or nil). Best-effort
// — partial failure leaves some sessions alive, but they'll naturally expire
// and the live db.Account.Disabled check kicks them out before they can act.
func (s *SessionStore) RevokeAllForAccount(ctx context.Context, accountID int32) (int, error) {
	pattern := fmt.Sprintf("session:%d:*", accountID)
	var (
		cursor   uint64
		deleted  int
		firstErr error
	)
	for {
		result, err := s.kv.ScanEntries(ctx, pattern, cursor, 100)
		if err != nil {
			return deleted, fmt.Errorf("session: scan: %w", err)
		}
		for _, entry := range result.Entries {
			if err := s.kv.Del(ctx, entry.Key); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			deleted++
		}
		cursor = result.NextCursor
		if cursor == 0 {
			break
		}
	}
	return deleted, firstErr
}

// CookieValue formats the session cookie's value: "<account_id>.<token>".
// Both halves are required at LoadSession time to construct the KV key.
// Only the random token portion is secret; account_id leaks nothing.
func CookieValue(accountID int32, token string) string {
	return fmt.Sprintf("%d.%s", accountID, token)
}

// ParseCookieValue splits "<account_id>.<token>" into its parts.
// Returns ok=false for any malformed input (no normalization).
func ParseCookieValue(v string) (int32, string, bool) {
	dot := strings.IndexByte(v, '.')
	if dot < 1 || dot == len(v)-1 {
		return 0, "", false
	}
	id, err := strconv.ParseInt(v[:dot], 10, 32)
	if err != nil || id <= 0 {
		return 0, "", false
	}
	return int32(id), v[dot+1:], true
}
