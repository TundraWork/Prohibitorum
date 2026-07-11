// Package pairing implements the device-pairing short-code ceremony for
// adding a passkey from a second device. Used to add a new device's passkey
// to an existing account without transferring a bearer token (URL) between
// devices.
//
// Flow:
//  1. New device (PC) calls NewPairing(); receives pairingID + human code.
//  2. PC displays the code; polls Get(pairingID) waiting for approval.
//  3. User opens /me on an authenticated device, enters the code,
//     server calls ApproveByCode(code, accountID) → binds pairing to user.
//  4. PC sees status=approved on next poll, runs the WebAuthn ceremony,
//     calls Consume(pairingID).
//
// Security:
//   - The code is NOT a bearer token. It's a label for a specific pending
//     pairing initiated by a specific browser session. Intercepting it
//     does not let an attacker register their device — the pairing they'd
//     need is bound to the originating PC's session.
//   - Approving on /me requires an authenticated session (gate enforced
//     by the calling handler).
//   - Short TTL (PairingTTL) bounds the brute-force / interception window.
//
// Storage: ephemeral; in-memory KV with TTL is enough. No DB rows.
package pairing

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/kv"
)

// PairingTTL bounds how long a code is valid. Short by design: this is a
// pairing handshake, not a long-lived invitation.
const PairingTTL = 5 * time.Minute

// codeAlphabet is base32 minus the four most easily-misread characters
// (0/O, 1/I/L). 26 letters minus those four plus 8 digits = 30 chars,
// 5 bits per char with rejection sampling. 8 chars ≈ 40 bits of entropy —
// brute-force impractical given the 5-min TTL and approval gate.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
const codeLen = 8

// PairingStatus is the state-machine label persisted with the pairing.
type PairingStatus string

const (
	PairingPending   PairingStatus = "pending"
	PairingApproved  PairingStatus = "approved"
	PairingConsumed  PairingStatus = "consumed"
)

// Pairing is the KV-persisted state for one pairing attempt. Two keys point
// at it: pairing:id:<id> (canonical record) and pairing:code:<code> (lookup
// from approve endpoint). Both share the same TTL.
//
// IMPORTANT: pairing does NOT carry a WebAuthn ceremony. It only proves "the
// holder of an authenticated session on Device A authorized issuing a
// session to whichever device generated this code". The new device, once
// signed in, runs the existing /me/credentials/register flow with the
// CORRECT user handle to register a local passkey. Doing WebAuthn at pair
// time would require a user handle before the account is known, and any
// substituted handle gets locked into the authenticator's local storage —
// breaking future discoverable login on that device.
type Pairing struct {
	ID           string               `json:"id"`
	Code         string               `json:"code"`
	Status       PairingStatus        `json:"status"`
	ApprovedFor  int32                `json:"approved_for,omitempty"` // 0 until approved
	InitiatorUA  string               `json:"initiator_ua,omitempty"`
	InitiatorIP  string               `json:"initiator_ip,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
	ExpiresAt    time.Time            `json:"expires_at"`
}

// PairingStore wraps a KV with the pairing key prefixes and JSON marshalling.
// Constructed once per Server.
type PairingStore struct {
	kv kv.Store
}

func NewPairingStore(store kv.Store) *PairingStore {
	return &PairingStore{kv: store}
}

func pairingIDKey(id string) string   { return "pairing:id:" + id }
func pairingCodeKey(code string) string { return "pairing:code:" + code }

// New creates a pending pairing and persists it. Returns the just-created
// object; the caller (HTTP handler) returns code + pairingID to the PC.
func (s *PairingStore) New(ctx context.Context, ua, ip string) (*Pairing, error) {
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	code, err := randomCode()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	p := &Pairing{
		ID:          id,
		Code:        code,
		Status:      PairingPending,
		InitiatorUA: ua,
		InitiatorIP: ip,
		CreatedAt:   now,
		ExpiresAt:   now.Add(PairingTTL),
	}
	if err := s.put(ctx, p); err != nil {
		return nil, err
	}
	// Code → id pointer, same TTL. Approve endpoint reads this first.
	if err := s.kv.SetEx(ctx, pairingCodeKey(code), id, PairingTTL); err != nil {
		// Best-effort cleanup; the canonical record will time out anyway.
		_ = s.kv.Del(ctx, pairingIDKey(id))
		return nil, fmt.Errorf("pairing: store code index: %w", err)
	}
	return p, nil
}

// GetByID returns the pairing or ErrPairingNotFound if unknown / expired.
func (s *PairingStore) GetByID(ctx context.Context, id string) (*Pairing, error) {
	raw, err := s.kv.Get(ctx, pairingIDKey(id))
	if err != nil {
		if err == kv.ErrKeyNotFound {
			return nil, authn.ErrPairingNotFound()
		}
		return nil, fmt.Errorf("pairing: kv get: %w", err)
	}
	var p Pairing
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("pairing: unmarshal: %w", err)
	}
	return &p, nil
}

// LookupByCode resolves a user-typed code (case-insensitive, hyphens
// stripped) to the underlying pairing. Returns ErrPairingNotFound on miss
// or expiry — same error so an attacker can't probe which codes have ever
// existed.
func (s *PairingStore) LookupByCode(ctx context.Context, code string) (*Pairing, error) {
	normalized := NormalizePairingCode(code)
	if len(normalized) != codeLen {
		return nil, authn.ErrPairingNotFound()
	}
	id, err := s.kv.Get(ctx, pairingCodeKey(normalized))
	if err != nil {
		if err == kv.ErrKeyNotFound {
			return nil, authn.ErrPairingNotFound()
		}
		return nil, fmt.Errorf("pairing: kv get code: %w", err)
	}
	return s.GetByID(ctx, id)
}

// Approve transitions a pending pairing to approved and binds it to
// accountID, atomically replacing the exact pending JSON with the approved
// JSON via KV CompareAndSwap while preserving the remaining TTL. This
// serializes concurrent approvals: exactly one caller's CAS matches the
// pending bytes and wins; every rival sees either the winner's approved
// bytes (a mismatch) or no bytes (expired/consumed) and fails closed with
// ErrPairingState.
//
// Idempotent within the winning account: if the canonical record already
// records this account as approved, Approve is a no-op. A re-approval by a
// different account is rejected — the CAS expected value is the pending
// bytes (not the rival's approved bytes), so it cannot match.
func (s *PairingStore) Approve(ctx context.Context, p *Pairing, accountID int32) error {
	// Fast path: the caller already holds the canonical approved state for
	// this account — idempotent no-op. We only trust this after the caller
	// read the canonical record (not a stale pre-approve object), so the
	// HTTP handler must reload via GetByID/LookupByCode before relying on
	// this branch.
	if p.Status == PairingApproved && p.ApprovedFor == accountID {
		return nil
	}
	if p.Status != PairingPending {
		return authn.ErrPairingState()
	}
	// Marshal the exact bytes the caller believes are stored (pending) and
	// the bytes to swap in (approved). CAS is byte-exact, so a rival that
	// approves between the caller's read and write changes the stored bytes
	// and makes the caller's CAS mismatch.
	expected, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("pairing: marshal pending: %w", err)
	}
	remaining := time.Until(p.ExpiresAt)
	if remaining <= 0 {
		return authn.ErrPairingExpired()
	}
	updated := *p
	updated.Status = PairingApproved
	updated.ApprovedFor = accountID
	newRaw, err := json.Marshal(&updated)
	if err != nil {
		return fmt.Errorf("pairing: marshal approved: %w", err)
	}
	swapped, err := s.kv.CompareAndSwap(ctx, pairingIDKey(p.ID), string(expected), string(newRaw), remaining)
	if err != nil {
		return fmt.Errorf("pairing: cas approve: %w", err)
	}
	if !swapped {
		// The canonical record no longer matches the pending bytes we
		// read — a rival approved, it was consumed/cancelled, or it
		// expired. Fail closed; the handler surfaces pairing_state.
		return authn.ErrPairingState()
	}
	// Reflect the transition in the caller's in-memory copy.
	p.Status = PairingApproved
	p.ApprovedFor = accountID
	return nil
}

// Consume atomically transitions an approved pairing to consumed via KV
// CompareAndSwap, replacing the exact approved JSON with a consumed marker
// that carries the remaining TTL. This makes consume single-winner: only
// the caller whose CAS matches the exact approved bytes wins; every rival
// sees the consumed marker (a mismatch) or no key (expired/deleted) and
// fails. The consumed marker remains in KV until TTL expiry to prevent
// resurrection — a second Consume or a re-Approve cannot match the approved
// bytes the marker replaced.
//
// A pending pairing is NOT mutated: Consume reads the canonical record to
// check state, and a pending record's bytes never match the expected
// approved JSON the CAS targets, so the CAS is a no-op and the caller
// receives ErrPairingNotApproved. The ceremony is preserved for the user
// to approve. A malformed record similarly fails the CAS (garbage never
// matches approved JSON) and the key is left intact. The code-index key is
// deleted only by the winner after the CAS succeeds.
//
// The handler must complete session issuance before responding; a failure
// after Consume does NOT resurrect the consumed state — the consumed marker
// persists until TTL.
func (s *PairingStore) Consume(ctx context.Context, id string) (*Pairing, error) {
	// Read the canonical record to get the exact approved bytes for CAS
	// and to check state before attempting the swap. This read is not
	// authoritative — the CAS is — but it lets us return precise errors
	// (pairing_not_approved for pending, pairing_not_found for absent)
	// without a destructive Pop.
	raw, err := s.kv.Get(ctx, pairingIDKey(id))
	if err != nil {
		if err == kv.ErrKeyNotFound {
			return nil, authn.ErrPairingNotFound()
		}
		return nil, fmt.Errorf("pairing: kv get: %w", err)
	}
	var p Pairing
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		// Malformed canonical record — fail closed. We do NOT Pop or CAS;
		// the garbage key remains so a latent bug is observable, but it
		// cannot be consumed (the CAS would never match anyway).
		return nil, authn.ErrPairingNotFound()
	}
	if p.Status != PairingApproved {
		// Pending or already-consumed. Do NOT mutate: a pending pairing
		// must survive so the user can still approve it; a consumed
		// marker is already terminal. Surface the precise error so the
		// handler returns the correct HTTP status.
		if p.Status == PairingPending {
			return nil, authn.ErrPairingNotApproved()
		}
		return nil, authn.ErrPairingState()
	}
	// Marshal the exact approved bytes the caller read (CAS expected) and
	// the consumed marker to swap in. CAS is byte-exact, so a concurrent
	// winner that already consumed changes the stored bytes and makes
	// this caller's CAS mismatch.
	expected := raw // exact bytes from the Get — no re-marshal drift
	remaining := time.Until(p.ExpiresAt)
	if remaining <= 0 {
		return nil, authn.ErrPairingExpired()
	}
	consumed := p
	consumed.Status = PairingConsumed
	newRaw, err := json.Marshal(&consumed)
	if err != nil {
		return nil, fmt.Errorf("pairing: marshal consumed: %w", err)
	}
	swapped, err := s.kv.CompareAndSwap(ctx, pairingIDKey(id), expected, string(newRaw), remaining)
	if err != nil {
		return nil, fmt.Errorf("pairing: cas consume: %w", err)
	}
	if !swapped {
		// A rival won the CAS — either consumed (now consumed marker) or
		// re-approved/cancelled (different bytes). Fail closed; the
		// handler surfaces the error without issuing a session.
		return nil, authn.ErrPairingState()
	}
	// Best-effort cleanup of the secondary code-index key. The consumed
	// marker blocks a second consume, so this Del is defense-in-depth; if
	// it fails the code key expires with its original TTL.
	_ = s.kv.Del(ctx, pairingCodeKey(p.Code))
	return &p, nil
}

// Cancel removes a pending pairing. Called when the user clicks "cancel"
// on /me, or when the PC abandons the flow before approval.
func (s *PairingStore) Cancel(ctx context.Context, p *Pairing) error {
	_ = s.kv.Del(ctx, pairingIDKey(p.ID))
	_ = s.kv.Del(ctx, pairingCodeKey(p.Code))
	return nil
}

func (s *PairingStore) put(ctx context.Context, p *Pairing) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("pairing: marshal: %w", err)
	}
	// Use the original TTL window — never extend it on update.
	remaining := time.Until(p.ExpiresAt)
	if remaining <= 0 {
		return authn.ErrPairingExpired()
	}
	if err := s.kv.SetEx(ctx, pairingIDKey(p.ID), string(raw), remaining); err != nil {
		return fmt.Errorf("pairing: kv put: %w", err)
	}
	return nil
}

// NormalizePairingCode trims whitespace and hyphens, upper-cases, so the
// user can type the code with or without the visual separators we render
// it with on the PC ("ABCD-EFGH" → "ABCDEFGH").
func NormalizePairingCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// FormatPairingCode renders an 8-char code as "XXXX-XXXX" for display.
func FormatPairingCode(s string) string {
	if len(s) != codeLen {
		return s
	}
	return s[:4] + "-" + s[4:]
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("pairing: random id: %w", err)
	}
	// base32-ish hex for URL-safety; pairing IDs are not user-visible.
	const hex = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, by := range b {
		out[i*2] = hex[by>>4]
		out[i*2+1] = hex[by&0xf]
	}
	return string(out), nil
}

func randomCode() (string, error) {
	out := make([]byte, codeLen)
	// Rejection-sample so distribution is uniform across the 30-char
	// alphabet. Each byte we read has 256 possible values; we accept only
	// those that fall into 0..29*9 (= 270), then mod 30. The reject rate
	// is ~5% so the loop is bounded in practice.
	const accept = byte(255 - (255 % 30))
	buf := make([]byte, 1)
	for i := 0; i < codeLen; {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("pairing: random code: %w", err)
		}
		if buf[0] >= accept {
			continue
		}
		out[i] = codeAlphabet[buf[0]%30]
		i++
	}
	return string(out), nil
}
