// Package auth — pairing.go
//
// Device-pairing-by-short-code flow. Used to add a new device's passkey to
// an existing account without transferring a bearer token (URL) between
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
// accountID. Idempotent within the same account; rejects re-approval by a
// different account.
func (s *PairingStore) Approve(ctx context.Context, p *Pairing, accountID int32) error {
	if p.Status == PairingApproved && p.ApprovedFor == accountID {
		return nil // idempotent
	}
	if p.Status != PairingPending {
		return authn.ErrPairingState()
	}
	p.Status = PairingApproved
	p.ApprovedFor = accountID
	return s.put(ctx, p)
}

// Consume marks the pairing consumed and deletes both KV keys. Called by
// the new device after the WebAuthn registration ceremony completes
// successfully; prevents the same pairing from being used twice.
func (s *PairingStore) Consume(ctx context.Context, p *Pairing) error {
	if p.Status != PairingApproved {
		return authn.ErrPairingState()
	}
	_ = s.kv.Del(ctx, pairingIDKey(p.ID))
	_ = s.kv.Del(ctx, pairingCodeKey(p.Code))
	return nil
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
