package oidc

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

// RefreshTokenTTL is the DEFAULT lifetime of a refresh token (and its family
// record) in KV, used when oidc.refresh_token_ttl is unset. 30 days is a
// conventional refresh lifetime; rotation slides this window forward on each
// successful exchange so an actively-used family persists, while an abandoned
// one expires. Callers pass the effective TTL (p.refreshTokenTTL()) into
// issueRefresh / rotateRefresh; never read this const directly at a store site.
const RefreshTokenTTL = 30 * 24 * time.Hour

// errRefreshInvalid is returned when a presented refresh token does not resolve
// to a live family — it never existed, expired, was revoked, or is a legacy
// pre-prt1 token that is no longer accepted after deployment. Callers map this
// to the OAuth invalid_grant error.
var errRefreshInvalid = errors.New("oidc: refresh token invalid")

// errRefreshReuse is returned when a superseded (already-rotated) refresh token
// is presented outside the idempotency grace window. This is the reuse-detection
// trip: the entire family is revoked as a side effect, defeating a stolen-token
// replay (OAuth 2.0 Security BCP §4.13.2). Callers map this to invalid_grant.
var errRefreshReuse = errors.New("oidc: refresh token reuse detected")

// refreshIdempotencyWindow bounds how long a just-rotated (previous) refresh
// token may be re-presented to receive the SAME successor instead of tripping
// reuse detection. Covers benign client double-submit / network retry. Kept
// short: within this window a stolen previous token could also redeem the
// successor, an accepted tradeoff (the token is already compromised in that
// case, and false family revocation is user-hostile).
const refreshIdempotencyWindow = 10 * time.Second

// refreshFamilyVersion is the family-record schema version. Increment if the
// record shape changes incompatibly.
const refreshFamilyVersion = 1

// refreshTokenPrefix is the versioned token-format prefix. Tokens without it
// are legacy and always rejected as invalid_grant after deployment.
const refreshTokenPrefix = "prt1."

// refreshSuccessorAADLabel prefixes the AES-GCM AAD used to bind the encrypted
// successor to (family_id, revision) so a ciphertext from one family or
// revision cannot be replayed against another.
const refreshSuccessorAADLabel = "refresh_successor"

// refreshFamily is the per-family record for a chain of rotated refresh tokens.
// It stores SHA-256 hashes of the current and previous token secrets (never
// the plaintext secrets), an AES-256-GCM encrypted copy of the current
// successor token (for idempotent replay recovery), the DEK version used to
// seal that successor, and a monotonically increasing revision used as the CAS
// guard. The remaining fields are the authentication snapshot carried forward
// into each refreshed access/ID token.
type refreshFamily struct {
	Version            int       `json:"version"`
	Revision           uint64    `json:"revision"`
	FamilyID           string    `json:"family_id"`
	CurrentHash        [32]byte  `json:"current_hash"`
	PreviousHash       [32]byte  `json:"previous_hash,omitempty"`
	EncryptedSuccessor string    `json:"encrypted_successor,omitempty"`
	DEKVersion         int32     `json:"dek_version,omitempty"`
	PreviousValidUntil time.Time `json:"previous_valid_until,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	LastUsedAt         time.Time `json:"last_used_at"`
	AbsoluteExpiresAt  time.Time `json:"absolute_expires_at"`
	InactiveExpiresAt  time.Time `json:"inactive_expires_at"`
	ClientID           string    `json:"client_id"`
	AccountID          int32     `json:"account_id"`
	SessionID          string    `json:"session_id"`
	Scope              []string  `json:"scope"`
	AuthTime           time.Time `json:"auth_time"`
	AMR                []string  `json:"amr"`
	ACR                string    `json:"acr"`
}

// refreshFamilyKey is the KV key under which a family record is stored. This is
// the ONLY key per family — there are no token→family mapping keys. The family
// ID is embedded in the token (prt1.<fid>.<secret>), so the record can be
// loaded directly without a mapping lookup.
func refreshFamilyKey(fid string) string { return "oidc:refresh:family:" + fid }

// randBytes returns n bytes of cryptographic randomness, base64url-encoded
// without padding so it is URL-safe. Used for family identifiers and token
// secrets.
func randBytes(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashSecret returns the SHA-256 hash of a refresh token secret.
func hashSecret(secret []byte) [32]byte {
	return sha256.Sum256(secret)
}

// parseRefreshToken splits a prt1.<base64url-family-id>.<base64url-secret>
// token into its family ID (as the base64url string stored in the record) and
// the raw secret bytes. Any token without the prt1 prefix or with an invalid
// structure returns errRefreshInvalid — this is how legacy-format tokens are
// rejected after deployment.
func parseRefreshToken(token string) (familyID string, secret []byte, err error) {
	if !strings.HasPrefix(token, refreshTokenPrefix) {
		return "", nil, errRefreshInvalid
	}
	rest := token[len(refreshTokenPrefix):]
	idx := strings.IndexByte(rest, '.')
	if idx <= 0 || idx == len(rest)-1 {
		return "", nil, errRefreshInvalid
	}
	familyID = rest[:idx]
	secret, derr := base64.RawURLEncoding.DecodeString(rest[idx+1:])
	if derr != nil {
		return "", nil, errRefreshInvalid
	}
	if len(secret) != 32 {
		return "", nil, errRefreshInvalid
	}
	return familyID, secret, nil
}

// buildRefreshToken assembles a prt1.<fid>.<base64url-secret> token from its
// components.
func buildRefreshToken(familyID string, secret []byte) string {
	return refreshTokenPrefix + familyID + "." + base64.RawURLEncoding.EncodeToString(secret)
}

// mintRefreshSecret generates a fresh 32-byte secret and returns it as both
// raw bytes (for hashing/encryption) and the full prt1 token string.
func mintRefreshToken(familyID string) (token string, secret []byte, err error) {
	secret = make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", nil, fmt.Errorf("oidc: generate refresh token secret: %w", err)
	}
	return buildRefreshToken(familyID, secret), secret, nil
}

// refreshSuccessorAAD builds the AES-GCM additional authenticated data that
// binds an encrypted successor to its family ID and revision, so a ciphertext
// lifted from one family or replayed under a different revision fails
// decryption.
func refreshSuccessorAAD(familyID string, revision uint64) []byte {
	return []byte(refreshSuccessorAADLabel + ":" + familyID + ":" + strconv.FormatUint(revision, 10))
}

// sealSuccessor encrypts a successor refresh token under the DEK using
// AES-256-GCM, returning base64url(nonce || ciphertext). The AAD binds the
// ciphertext to (familyID, revision).
func sealSuccessor(dek []byte, successor, familyID string, revision uint64) (string, error) {
	if len(dek) != 32 {
		return "", fmt.Errorf("oidc: DEK must be 32 bytes (AES-256), got %d", len(dek))
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	aad := refreshSuccessorAAD(familyID, revision)
	ciphertext := aead.Seal(nil, nonce, []byte(successor), aad)
	combined := append(nonce, ciphertext...)
	return base64.RawURLEncoding.EncodeToString(combined), nil
}

// openSuccessor reverses sealSuccessor, returning the plaintext successor
// token. A GCM authentication failure (tampered ciphertext or wrong AAD/DEK)
// returns the underlying error so the caller can classify it.
func openSuccessor(dek []byte, encSuccessor, familyID string, revision uint64) (string, error) {
	if len(dek) != 32 {
		return "", fmt.Errorf("oidc: DEK must be 32 bytes (AES-256), got %d", len(dek))
	}
	raw, err := base64.RawURLEncoding.DecodeString(encSuccessor)
	if err != nil {
		return "", fmt.Errorf("oidc: decode encrypted successor: %w", err)
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := aead.NonceSize()
	if len(raw) < ns {
		return "", fmt.Errorf("oidc: encrypted successor too short")
	}
	nonce, ciphertext := raw[:ns], raw[ns:]
	aad := refreshSuccessorAAD(familyID, revision)
	pt, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return "", fmt.Errorf("oidc: decrypt refresh successor: %w", err)
	}
	return string(pt), nil
}

// activeDEK returns the highest-version DEK and its version from the DEK map,
// or (0, nil, false) if no DEKs are configured.
func activeDEK(deks map[int][]byte) (int32, []byte, bool) {
	if len(deks) == 0 {
		return 0, nil, false
	}
	maxVer := 0
	for v := range deks {
		if v > maxVer {
			maxVer = v
		}
	}
	return int32(maxVer), deks[maxVer], true
}

// putFamily writes the family record (JSON) under refreshFamilyKey with the
// given ttl. It is used to seed a new family on issue. Rotation uses CAS
// (compare-and-swap) rather than putFamily so that concurrent rotations of the
// same current token produce exactly one winner. Tests also use putFamily to
// manipulate family records directly.
func putFamily(ctx context.Context, store kv.Store, fam *refreshFamily, ttl time.Duration) error {
	payload, err := json.Marshal(fam)
	if err != nil {
		return fmt.Errorf("oidc: marshal refresh family: %w", err)
	}
	return store.SetEx(ctx, refreshFamilyKey(fam.FamilyID), string(payload), ttl)
}

// loadFamilyByFID loads a family record by family ID. A miss returns
// errRefreshInvalid; a malformed payload is a wrapped decode error.
func loadFamilyByFID(ctx context.Context, store kv.Store, familyID string) (*refreshFamily, error) {
	raw, err := store.Get(ctx, refreshFamilyKey(familyID))
	if err != nil {
		if errors.Is(err, kv.ErrKeyNotFound) {
			return nil, errRefreshInvalid
		}
		return nil, err
	}
	var fam refreshFamily
	if err := json.Unmarshal([]byte(raw), &fam); err != nil {
		return nil, fmt.Errorf("oidc: unmarshal refresh family: %w", err)
	}
	return &fam, nil
}

// loadFamily parses a presented prt1 token, extracts the family ID, and loads
// the family record. A miss (family revoked/expired/never-existed) returns
// errRefreshInvalid; a token without the prt1 prefix (legacy) also returns
// errRefreshInvalid. A malformed family payload is a wrapped decode error.
func loadFamily(ctx context.Context, store kv.Store, presented string) (*refreshFamily, error) {
	fid, _, err := parseRefreshToken(presented)
	if err != nil {
		return nil, err
	}
	return loadFamilyByFID(ctx, store, fid)
}

// issueRefresh seeds a NEW refresh-token family. It takes fam by value,
// generates a random FamilyID and a 32-byte secret, builds a prt1 token,
// stamps CreatedAt/LastUsedAt/AbsoluteExpiresAt/InactiveExpiresAt, and writes
// the family record with the given ttl. It returns the freshly minted token and
// the generated family id. The caller need not pre-populate FamilyID; it is set
// here. No DEKs are needed at issue time (there is no successor to encrypt).
func issueRefresh(ctx context.Context, store kv.Store, fam refreshFamily, ttl time.Duration) (token string, familyID string, err error) {
	fid, err := randBytes(32)
	if err != nil {
		return "", "", fmt.Errorf("oidc: generate refresh family id: %w", err)
	}
	fam.FamilyID = fid
	fam.Version = refreshFamilyVersion
	fam.CreatedAt = time.Now().UTC()
	fam.LastUsedAt = fam.CreatedAt
	fam.AbsoluteExpiresAt = fam.CreatedAt.Add(ttl)
	fam.InactiveExpiresAt = fam.CreatedAt.Add(ttl)

	token, secret, err := mintRefreshToken(fid)
	if err != nil {
		return "", "", err
	}
	fam.CurrentHash = hashSecret(secret)
	fam.Revision = 0

	if err := putFamily(ctx, store, &fam, ttl); err != nil {
		return "", "", err
	}
	return token, fid, nil
}

// rotateRefresh performs a single-use exchange of a refresh token, made atomic
// by compare-and-swap (CAS) on the family record. No SetNX lock is needed — CAS
// ensures exactly one winner among concurrent rotations of the same current
// token.
//
//  1. Parse the presented token → (familyID, secret). A token without the
//     prt1 prefix (legacy) → errRefreshInvalid.
//  2. Load the family record by familyID. A miss → errRefreshInvalid.
//  3. Hash the presented secret and constant-time compare against
//     CurrentHash and PreviousHash:
//     - Matches PreviousHash and within PreviousValidUntil → idempotent
//       replay: decrypt the EncryptedSuccessor and return it (rotated=false).
//       No second mint, no CAS.
//     - Matches CurrentHash → mint a new successor, encrypt it under the
//       active DEK, update hashes/revision/encrypted-successor, CAS the old
//       record → new record. On CAS loss, reload and classify:
//       • Family gone → errRefreshInvalid (someone revoked it).
//       • Presented hash now matches PreviousHash and within window →
//         concurrent exchange won by another caller; decrypt and return the
//         successor (idempotent replay, rotated=false).
//       • Otherwise → reuse: revoke family, errRefreshReuse.
//     - Matches neither (or previous outside window) → reuse: revoke family,
//       errRefreshReuse. reuseAccountID carries the family's AccountID for
//       audit attribution.
//
// rotated reports whether this call performed a real rotation (true) vs served
// an idempotent replay (false). reuseAccountID is non-zero only when
// errRefreshReuse is returned.
func rotateRefresh(ctx context.Context, store kv.Store, deks map[int][]byte, presented string, ttl time.Duration) (fam *refreshFamily, newToken string, rotated bool, reuseAccountID int32, err error) {
	fid, secret, err := parseRefreshToken(presented)
	if err != nil {
		return nil, "", false, 0, err
	}

	fam, err = loadFamilyByFID(ctx, store, fid)
	if err != nil {
		return nil, "", false, 0, err
	}

	now := time.Now().UTC()
	presentedHash := hashSecret(secret)

	// Idempotent replay: the presented token is the previous token and still
	// within the grace window. Return the already-rotated successor without a
	// second mint.
	if now.Before(fam.PreviousValidUntil) && subtle.ConstantTimeCompare(presentedHash[:], fam.PreviousHash[:]) == 1 {
		successor, derr := decryptFamilySuccessor(deks, fam)
		if derr != nil {
			return nil, "", false, 0, derr
		}
		return fam, successor, false, 0, nil
	}

	// Reuse detection: the presented token is neither the current token nor a
	// valid in-window previous token. Revoke the whole family.
	if subtle.ConstantTimeCompare(presentedHash[:], fam.CurrentHash[:]) != 1 {
		accountID := fam.AccountID
		_ = store.Del(ctx, refreshFamilyKey(fam.FamilyID))
		return nil, "", false, accountID, errRefreshReuse
	}

	// Current token: mint successor, encrypt it, CAS the record.
	for {
		oldPayload, mErr := marshalFamily(fam)
		if mErr != nil {
			return nil, "", false, 0, mErr
		}

		successorToken, successorSecret, mErr := mintRefreshToken(fid)
		if mErr != nil {
			return nil, "", false, 0, mErr
		}

		dekVer, dek, dekOK := activeDEK(deks)
		if !dekOK {
			return nil, "", false, 0, fmt.Errorf("oidc: no data encryption keys configured for refresh rotation")
		}

		newRev := fam.Revision + 1
		encSuccessor, sErr := sealSuccessor(dek, successorToken, fid, newRev)
		if sErr != nil {
			return nil, "", false, 0, sErr
		}

		updated := *fam
		updated.Revision = newRev
		updated.CurrentHash = hashSecret(successorSecret)
		updated.PreviousHash = fam.CurrentHash
		updated.EncryptedSuccessor = encSuccessor
		updated.DEKVersion = dekVer
		updated.PreviousValidUntil = now.Add(refreshIdempotencyWindow)
		updated.LastUsedAt = now
		updated.InactiveExpiresAt = now.Add(ttl)

		newPayload, mErr := marshalFamily(&updated)
		if mErr != nil {
			return nil, "", false, 0, mErr
		}

		swapped, casErr := store.CompareAndSwap(ctx, refreshFamilyKey(fid), oldPayload, newPayload, ttl)
		if casErr != nil {
			return nil, "", false, 0, casErr
		}
		if swapped {
			return &updated, successorToken, true, 0, nil
		}

		// CAS lost: another caller modified the record. Reload and classify.
		reloaded, rErr := loadFamilyByFID(ctx, store, fid)
		if rErr != nil {
			// Family gone (revoked/expired) → invalid.
			return nil, "", false, 0, rErr
		}

		// If the presented token is now the previous token and within the
		// grace window, a concurrent exchange won. Serve the idempotent
		// successor from the reloaded record.
		rNow := time.Now().UTC()
		if rNow.Before(reloaded.PreviousValidUntil) && subtle.ConstantTimeCompare(presentedHash[:], reloaded.PreviousHash[:]) == 1 {
			successor, derr := decryptFamilySuccessor(deks, reloaded)
			if derr != nil {
				return nil, "", false, 0, derr
			}
			return reloaded, successor, false, 0, nil
		}

		// The presented token no longer matches current or valid-previous.
		// This is reuse (or the family was rotated further by another path).
		accountID := reloaded.AccountID
		_ = store.Del(ctx, refreshFamilyKey(fid))
		return nil, "", false, accountID, errRefreshReuse
	}
}

// marshalFamily returns the canonical JSON bytes for a family record. Used to
// produce the exact expected-value for CAS.
func marshalFamily(fam *refreshFamily) (string, error) {
	payload, err := json.Marshal(fam)
	if err != nil {
		return "", fmt.Errorf("oidc: marshal refresh family: %w", err)
	}
	return string(payload), nil
}

// decryptFamilySuccessor decrypts the family's EncryptedSuccessor using the
// DEK version stored in the record (not the active DEK), so successors sealed
// under an older DEK remain recoverable after DEK rotation.
func decryptFamilySuccessor(deks map[int][]byte, fam *refreshFamily) (string, error) {
	dek, ok := deks[int(fam.DEKVersion)]
	if !ok {
		return "", fmt.Errorf("oidc: missing DEK version %d for refresh family %q", fam.DEKVersion, fam.FamilyID)
	}
	return openSuccessor(dek, fam.EncryptedSuccessor, fam.FamilyID, fam.Revision)
}

// lookupRefresh resolves a presented prt1 token to its family WITHOUT mutating
// anything — no rotation, no revocation, no TTL change. It parses the family
// ID from the token and loads the record. Returns (family, true) when the
// family record resolves, else (nil, false). Used by /introspect and /revoke,
// which must not consume the token. A legacy token (no prt1 prefix) resolves
// to (nil, false).
func lookupRefresh(ctx context.Context, store kv.Store, presented string) (*refreshFamily, bool) {
	fam, err := loadFamily(ctx, store, presented)
	if err != nil {
		return nil, false
	}
	return fam, true
}

// isActiveToken reports whether the presented token is still a valid member of
// the family for read-only purposes (introspection): its secret hash must
// match the current hash, or the previous hash while still inside the
// idempotency window. A legacy token (no prt1 prefix) is never active.
func (fam *refreshFamily) isActiveToken(presented string, now time.Time) bool {
	_, secret, err := parseRefreshToken(presented)
	if err != nil {
		return false
	}
	h := hashSecret(secret)
	if subtle.ConstantTimeCompare(h[:], fam.CurrentHash[:]) == 1 {
		return true
	}
	return now.Before(fam.PreviousValidUntil) && subtle.ConstantTimeCompare(h[:], fam.PreviousHash[:]) == 1
}

// revokeFamily deletes a family record by id, invalidating every token in the
// chain (subsequent rotate/lookup of any of them misses the family →
// errRefreshInvalid / false). Deleting an absent family is a no-op.
func revokeFamily(ctx context.Context, store kv.Store, familyID string) error {
	return store.Del(ctx, refreshFamilyKey(familyID))
}

// grantRefreshToken implements the RFC 6749 §6 refresh_token grant. It rotates
// the presented token (single-use; reuse trips family revocation), or — for a
// just-rotated token re-presented within the idempotency window — serves the
// same successor without a second mint. Either way it re-checks the bound
// client and account and re-issues a fresh access + ID token plus the
// (rotated or replayed) refresh token. The re-issued ID token carries the
// family's snapshotted auth_time/amr/acr/sid and omits nonce (none is
// snapshotted).
func (p *Provider) grantRefreshToken(w http.ResponseWriter, r *http.Request, client db.OidcClient) {
	ctx := r.Context()
	presented := r.PostForm.Get("refresh_token")

	fam, newToken, rotated, reuseAccountID, err := rotateRefresh(ctx, p.kv, p.deks, presented, p.refreshTokenTTL())
	if errors.Is(err, errRefreshReuse) {
		var acctPtr *int32
		if reuseAccountID != 0 {
			acctPtr = &reuseAccountID
		}
		p.auditTokenEvent(ctx, r, audit.EventFail, acctPtr, map[string]any{
			"reason":    "refresh_reuse",
			"client_id": client.ClientID,
		})
		writeOIDCError(w, r, http.StatusBadRequest, errCodeInvalidGrant, "refresh token reuse detected")
		return
	}
	if err != nil {
		writeOIDCError(w, r, http.StatusBadRequest, errCodeInvalidGrant, "invalid refresh token")
		return
	}

	// Client binding (RFC 6749 §6): a refresh token presented by a client other
	// than the one it was issued to is anomalous. The family is live (rotated or
	// served an idempotent replay) — revoke the whole family (treat the mismatch
	// as an attack) before refusing.

	if fam.ClientID != client.ClientID {
		_ = revokeFamily(ctx, p.kv, fam.FamilyID)
		famAcctID := fam.AccountID
		p.auditTokenEvent(ctx, r, audit.EventFail, &famAcctID, map[string]any{
			"reason":    "code_client_mismatch",
			"client_id": client.ClientID,
		})
		writeOIDCError(w, r, http.StatusBadRequest, errCodeInvalidGrant, "client mismatch")
		return
	}

	// Re-check the account on every refresh: a disabled account's family must
	// die so the long-lived grant cannot outlive the account's standing.
	acct, err := p.queries.GetAccountByID(ctx, fam.AccountID)
	if err != nil {
		_ = revokeFamily(ctx, p.kv, fam.FamilyID)
		famAcctID := fam.AccountID
		p.auditTokenEvent(ctx, r, audit.EventFail, &famAcctID, map[string]any{
			"reason":    "account_unavailable",
			"client_id": client.ClientID,
		})
		writeOIDCError(w, r, http.StatusBadRequest, errCodeInvalidGrant, "account not found")
		return
	}
	if acct.Disabled {
		_ = revokeFamily(ctx, p.kv, fam.FamilyID)
		acctID := acct.ID
		p.auditTokenEvent(ctx, r, audit.EventFail, &acctID, map[string]any{
			"reason":    "account_unavailable",
			"client_id": client.ClientID,
		})
		writeOIDCError(w, r, http.StatusBadRequest, errCodeInvalidGrant, "account disabled")
		return
	}

	// Re-check per-app access on every refresh (RBAC): a grant that was revoked
	// — directly, via the user's removal from an authorized group, or by the
	// client being newly restricted — must NOT keep minting tokens off a
	// long-lived refresh family. No admin bypass. Fail CLOSED on a predicate
	// error (server_error, family preserved — we make no authorization claim).
	// On denial: durably revoke the whole rotating family so the cut survives
	// even though no new token is issued, then refuse with invalid_grant.
	authzed, aerr := p.queries.IsAccountAuthorizedForOIDCClient(ctx, db.IsAccountAuthorizedForOIDCClientParams{
		AccountID: pgtype.Int4{Int32: fam.AccountID, Valid: true},
		ClientID:  client.ClientID,
	})
	if aerr != nil {
		writeOIDCError(w, r, http.StatusInternalServerError, errCodeServerError, "could not evaluate access")
		return
	}
	if !authzed.Bool {
		_ = revokeFamily(ctx, p.kv, fam.FamilyID) // durable cut: kill the rotating family
		acctID := acct.ID
		p.auditTokenEvent(ctx, r, audit.EventAccessDenied, &acctID, map[string]any{
			"reason":    "app_access_denied",
			"client_id": client.ClientID,
		})
		writeOIDCError(w, r, http.StatusBadRequest, errCodeInvalidGrant, "not authorized for this application")
		return
	}

	now := time.Now()
	accessToken, idToken, err := p.mintAccessAndIDTokens(ctx, acct, client.ClientID, "" /*nonce*/, fam.SessionID, fam.ACR, fam.AMR, fam.Scope, fam.AuthTime, now)
	if err != nil {
		// The family is live (from this or a prior rotation). Returning no token
		// would leave it in a live-but-unusable state the client is locked out of.
		// Fail closed: revoke the family so the client cleanly re-authenticates
		// rather than wedging.
		_ = revokeFamily(ctx, p.kv, fam.FamilyID)
		writeOIDCError(w, r, http.StatusInternalServerError, errCodeServerError, "could not mint tokens")
		return
	}

	reason := "refresh_rotated"
	if !rotated {
		reason = "refresh_idempotent_replay"
	}
	acctID := acct.ID
	p.auditTokenEvent(ctx, r, audit.EventUse, &acctID, map[string]any{
		"reason":    reason,
		"client_id": client.ClientID,
	})

	writeTokenResponse(w, tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(p.accessTokenTTL().Seconds()),
		IDToken:      idToken,
		RefreshToken: newToken,
		Scope:        strings.Join(fam.Scope, " "),
	})
}
