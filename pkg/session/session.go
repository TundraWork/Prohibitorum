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

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/logx"
)

// SessionStore issues, loads, and revokes sessions backed by pkg/kv.
//
// Key layout:
//
//	session:<account_id>:<random>  → authn.SessionData JSON, TTL = SessionStore.ttl
//
// The account_id prefix lets RevokeAllForAccount enumerate by glob without
// maintaining a secondary index.
// SessionQueries is the subset of pkg/db.Querier that the session store needs.
// Declared locally so tests can supply a no-op stub without re-implementing the
// full sqlc-generated Querier surface.
type SessionQueries interface {
	InsertSession(ctx context.Context, arg db.InsertSessionParams) (db.Session, error)
	RevokeSession(ctx context.Context, id string) error
	RevokeAllSessionsByAccount(ctx context.Context, accountID int32) error
}

type SessionStore struct {
	kv      kv.Store
	q       SessionQueries // PG-persisted session metadata (auth_time, amr, acr, sid)
	ttl     time.Duration
	refresh time.Duration // sliding-refresh threshold; default ttl/4
}

// NewSessionStore constructs a store with the given session TTL.
// Sessions refresh themselves on Load when remaining time < ttl/4.
//
// q persists the immutable authentication facts (auth_time, amr, acr) per
// session ID so v0.4+ OIDC ID tokens can carry sid/amr/acr claims and v0.5+
// SAML SLO can enumerate active SP sessions.
func NewSessionStore(store kv.Store, q SessionQueries, ttl time.Duration) *SessionStore {
	return &SessionStore{kv: store, q: q, ttl: ttl, refresh: ttl / 4}
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

// Issue writes a fresh session to KV plus a row in the PG session table
// capturing the immutable authentication facts (auth_time, amr, acr). Returns
// the random KV token and the authn.SessionData that was stored.
//
// amr must list the RFC 8176 method values that produced this session:
//   - WebAuthn login or registration → ["hwk"]
//   - Password + TOTP (v0.2) → ["pwd","otp","mfa"]
//   - Upstream OIDC federation (v0.3) → ["federated"]
//
// If the PG insert fails, the KV entry is rolled back so the two stores stay
// consistent.
func (s *SessionStore) Issue(ctx context.Context, accountID int32, ip, ua string, amr []string) (string, *authn.SessionData, error) {
	if len(amr) == 0 {
		return "", nil, errors.New("session: Issue requires non-empty amr")
	}
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
	if _, err := s.q.InsertSession(ctx, db.InsertSessionParams{
		ID:        sessionID,
		AccountID: accountID,
		AuthTime:  pgtype.Timestamptz{Time: now, Valid: true},
		Amr:       amr,
	}); err != nil {
		_ = s.kv.Del(ctx, sessionKey(accountID, token))
		return "", nil, fmt.Errorf("session: insert pg: %w", err)
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

// Revoke deletes a specific session. The PG session row is soft-deleted
// (revoked_at stamped) so audit trails and OIDC sid-claim resolution still
// work after revocation. Best-effort on the PG side — a stale KV entry is
// already gone; a lingering active=false PG row gets pruned by sweep.
func (s *SessionStore) Revoke(ctx context.Context, accountID int32, token string) error {
	key := sessionKey(accountID, token)
	var sessionID string
	if raw, err := s.kv.Get(ctx, key); err == nil {
		var data authn.SessionData
		if jerr := json.Unmarshal([]byte(raw), &data); jerr == nil {
			sessionID = data.SessionID
		}
	}
	if err := s.kv.Del(ctx, key); err != nil {
		return err
	}
	if sessionID != "" {
		if err := s.q.RevokeSession(ctx, sessionID); err != nil {
			logx.WithContext(ctx).WithError(err).Warn("session: revoke pg failed (KV already deleted)")
		}
	}
	return nil
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
			if err := s.q.RevokeSession(ctx, sessionID); err != nil {
				logx.WithContext(ctx).WithError(err).Warn("session: revoke pg failed (KV already deleted)")
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
	if err := s.q.RevokeAllSessionsByAccount(ctx, accountID); err != nil {
		logx.WithContext(ctx).WithError(err).Warn("session: revoke-all pg failed (KV already cleared)")
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
