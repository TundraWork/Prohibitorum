// Package server — handle_pairing.go
//
// HTTP handlers for the device-pairing-by-short-code flow. See
// pkg/auth/pairing.go for the state machine and security argument.
//
// Pairing is session issuance only — it does NOT register a passkey. After
// the new device receives a session via /pair/complete, it runs the
// existing /me/credentials/register/{begin,complete} flow to register a
// local passkey for next time. Doing WebAuthn at pair time would lock a
// throwaway user handle into the authenticator and break future
// discoverable login on the new device.
//
// Endpoints:
//
//	POST /auth/devices/pair/begin    — anonymous. New device starts pairing.
//	GET  /auth/devices/pair/status   — anonymous. New device polls.
//	POST /auth/devices/pair/complete — anonymous. New device redeems an
//	                                    approved pairing for a session.
//	GET  /me/devices/pair/lookup     — authed. Show pairing context before
//	                                    confirmation.
//	POST /me/devices/pair/approve    — authed. Bind pairing to caller.
//	POST /me/devices/pair/cancel     — authed. Drop a pending pairing.
package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/credential/pairing"
	"prohibitorum/pkg/logx"
	sessstore "prohibitorum/pkg/session"
)

// ----- POST /auth/devices/pair/begin (anonymous) ---------------------------

type pairBeginResp struct {
	PairingID   string    `json:"pairingId"`
	Code        string    `json:"code"`        // raw 8-char
	DisplayCode string    `json:"displayCode"` // "XXXX-XXXX"
	ExpiresAt   time.Time `json:"expiresAt"`
}

func (s *Server) handlePairBeginHTTP(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP.IP(r)
	ua := r.UserAgent()
	p, err := s.pairingStore.New(r.Context(), ua, ip)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event":      "auth.pairing_begin",
		"pairing_id": p.ID,
		"client_ip":  ip,
	}).Info("auth")
	_ = s.Audit.Record(r.Context(), audit.Record{
		Factor: audit.FactorSession,
		Event:  audit.EventUse,
		Detail: map[string]any{"reason": "pairing_begin", "pairing_id": p.ID},
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pairBeginResp{
		PairingID:   p.ID,
		Code:        p.Code,
		DisplayCode: pairing.FormatPairingCode(p.Code),
		ExpiresAt:   p.ExpiresAt,
	})
}

// ----- GET /auth/devices/pair/status (anonymous, polled) -------------------

type pairStatusResp struct {
	Status    string    `json:"status"` // pending | approved | expired
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

func (s *Server) handlePairStatusHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeJSON(w, pairStatusResp{Status: "expired"})
		return
	}
	p, err := s.pairingStore.GetByID(r.Context(), id)
	if err != nil {
		// Not-found or expired both surface as "expired" — the PC's UI
		// reacts the same way (offer retry) and we don't leak whether the
		// id was ever valid.
		if ae := authn.AsAuthError(err); ae != nil && ae.Code == "pairing_not_found" {
			writeJSON(w, pairStatusResp{Status: "expired"})
			return
		}
		writeAuthErr(w, err)
		return
	}
	writeJSON(w, pairStatusResp{
		Status:    string(p.Status),
		ExpiresAt: p.ExpiresAt,
	})
}

// ----- POST /auth/devices/pair/complete (anonymous) ------------------------
//
// Redeems an approved pairing for a session cookie. Returns SessionView so
// the PC can route to the appropriate landing page. The PC then runs the
// existing /me/credentials/register flow to add a local passkey.

type pairCompleteReq struct {
	PairingID string `json:"pairingId"`
}

type pairCompleteResp struct {
	Session contract.SessionView `json:"session"`
}

func (s *Server) handlePairCompleteHTTP(w http.ResponseWriter, r *http.Request) {
	var body pairCompleteReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PairingID == "" {
		writeAuthErr(w, authn.ErrPairingNotFound())
		return
	}
	p, err := s.pairingStore.GetByID(r.Context(), body.PairingID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if p.Status != pairing.PairingApproved {
		writeAuthErr(w, authn.ErrPairingNotApproved())
		return
	}
	acct, err := s.queries.GetAccountByID(r.Context(), p.ApprovedFor)
	if err != nil {
		writeAuthErr(w, authn.ErrAccountNotFound())
		return
	}
	if acct.Disabled {
		writeAuthErr(w, authn.ErrAccountDisabled())
		return
	}
	if me := s.maintenanceLockout(r.Context(), acct.ID); me != nil {
		writeAuthErr(w, me)
		return
	}
	// Consume BEFORE issuing the session so a duplicate /complete cannot
	// double-issue if two concurrent requests both pass the status check
	// above. KV Del is single-key atomic; the loser sees pairing_not_found.
	if err := s.pairingStore.Consume(r.Context(), p); err != nil {
		writeAuthErr(w, err)
		return
	}
	ip := s.clientIP.IP(r)
	sessionToken, _, err := s.sessionStore.Issue(r.Context(), acct.ID, ip, r.UserAgent(), []string{"hwk"}, nil)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	http.SetCookie(w, sessstore.FreshSessionCookie(s.config, r, acct.ID, sessionToken, s.config.SessionTTL))

	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event":      "auth.pairing_completed",
		"account_id": acct.ID,
		"pairing_id": p.ID,
		"client_ip":  ip,
	}).Info("auth")
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: &acct.ID,
		Factor:    audit.FactorSession,
		Event:     audit.EventSessionStart,
		Detail:    map[string]any{"via": "pairing"},
	})

	writeJSON(w, pairCompleteResp{Session: s.sessionView(&acct)})
}

// ----- GET /me/devices/pair/lookup (authed) --------------------------------
//
// User types the code on /me; the lookup returns the pairing's initiator
// context (UA + IP + age + the formatted code itself) so the user can
// verify it matches what's on the other device before approving.

type pairLookupResp struct {
	PairingID    string    `json:"pairingId"`
	DisplayCode  string    `json:"displayCode"` // echo so /me UI can compare
	InitiatorUA  string    `json:"initiatorUa"`
	InitiatorIP  string    `json:"initiatorIp"`
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
	AlreadyBound bool      `json:"alreadyBound"`
}

func (s *Server) handlePairLookupHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	// Per-account cap on lookups — caps the brute-force surface against the
	// code space even though entropy already makes it impractical.
	if s.rateLimit(w, r, "pair_lookup:acct:"+strconv.Itoa(int(sess.Account.ID)), 20, time.Minute) {
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeAuthErr(w, authn.ErrPairingNotFound())
		return
	}
	p, err := s.pairingStore.LookupByCode(r.Context(), code)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	// If already approved for a different account, refuse to surface the
	// pairing — the code is no longer claimable by this caller.
	if p.Status == pairing.PairingApproved && p.ApprovedFor != sess.Account.ID {
		writeAuthErr(w, authn.ErrPairingNotFound())
		return
	}
	writeJSON(w, pairLookupResp{
		PairingID:    p.ID,
		DisplayCode:  pairing.FormatPairingCode(p.Code),
		InitiatorUA:  p.InitiatorUA,
		InitiatorIP:  p.InitiatorIP,
		CreatedAt:    p.CreatedAt,
		ExpiresAt:    p.ExpiresAt,
		AlreadyBound: p.Status == pairing.PairingApproved && p.ApprovedFor == sess.Account.ID,
	})
}

// ----- POST /me/devices/pair/approve (authed) ------------------------------

type pairApproveReq struct {
	Code string `json:"code"`
}

func (s *Server) handlePairApproveHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	// Per-account approve cap, tighter than lookup. A legit user approves
	// new devices rarely; high rates indicate a script or a hijacked session.
	if s.rateLimit(w, r, "pair_approve:acct:"+strconv.Itoa(int(sess.Account.ID)), 10, time.Minute) {
		return
	}
	// Approving a pairing permanently binds a new credential to the
	// account, so it's gated by a fresh WebAuthn assertion. A stolen
	// session cookie alone can't elevate past this — only possession of
	// the user's authenticator + biometric.
	if s.requireFreshSudo(r.Context(), w, sess) {
		return
	}
	var body pairApproveReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrPairingNotFound())
		return
	}
	p, err := s.pairingStore.LookupByCode(r.Context(), body.Code)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := s.pairingStore.Approve(r.Context(), p, sess.Account.ID); err != nil {
		writeAuthErr(w, err)
		return
	}
	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event":      "auth.pairing_approved",
		"pairing_id": p.ID,
		"account_id": sess.Account.ID,
		"client_ip":  s.clientIP.IP(r),
	}).Info("auth")
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: &sess.Account.ID,
		Factor:    audit.FactorSession,
		Event:     audit.EventUse,
		Detail:    map[string]any{"reason": "pairing_approved", "pairing_id": p.ID},
	})
	w.WriteHeader(http.StatusNoContent)
}

// ----- POST /me/devices/pair/cancel (authed) -------------------------------

func (s *Server) handlePairCancelHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	var body pairApproveReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrPairingNotFound())
		return
	}
	p, err := s.pairingStore.LookupByCode(r.Context(), body.Code)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	// Refuse to cancel a pairing approved by a different account.
	if p.Status == pairing.PairingApproved && p.ApprovedFor != sess.Account.ID {
		writeAuthErr(w, authn.ErrPairingNotFound())
		return
	}
	if err := s.pairingStore.Cancel(r.Context(), p); err != nil {
		writeAuthErr(w, err)
		return
	}
	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event":      "auth.pairing_cancelled",
		"pairing_id": p.ID,
		"account_id": sess.Account.ID,
	}).Info("auth")
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: &sess.Account.ID,
		Factor:    audit.FactorSession,
		Event:     audit.EventFail,
		Detail:    map[string]any{"reason": "pairing_cancelled", "pairing_id": p.ID},
	})
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
