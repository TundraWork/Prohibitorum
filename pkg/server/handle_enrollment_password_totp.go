// Package server — handle_enrollment_password_totp.go
//
// Password and TOTP enrollment in one request. The browser generates the TOTP
// secret and submits it with the password and current code. Invitation,
// federated registration, and reset writes remain atomic with token consumption.

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
	"prohibitorum/pkg/logx"
)

const (
	enrollMethodPasskey      = "passkey"
	enrollMethodPasswordTOTP = "password_totp"
)

func enrollmentAllowedMethods(intent string) []string {
	if intent == enrollment.IntentBootstrap {
		return []string{enrollMethodPasskey}
	}
	return []string{enrollMethodPasskey, enrollMethodPasswordTOTP}
}

// POST /api/prohibitorum/enrollments/{token}/password-totp/verify
func (s *Server) handleEnrollmentPasswordTOTPVerifyHTTP(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	q := s.enrollmentQ()
	e, err := enrollment.LoadEnrollment(r.Context(), q, token)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if e.Intent == enrollment.IntentBootstrap {
		writeAuthErr(w, authn.ErrEnrollmentMethodNotAllowed())
		return
	}

	var body struct {
		Username     string `json:"username,omitempty"`
		DisplayName  string `json:"displayName,omitempty"`
		Password     string `json:"password"`
		SecretBase32 string `json:"secret_base32"`
		Code         string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
		len(body.Password) < 8 || len(body.Password) > 1024 ||
		body.SecretBase32 == "" || body.Code == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	matchedStep, ok := s.totpStore.VerifyCandidateSecret(body.SecretBase32, body.Code)
	if !ok {
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}
	passwordPHC, err := s.passwordStore.Hash(body.Password)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify: hash: %w", err))
		return
	}

	var proposal *newAccountCeremony
	switch e.Intent {
	case enrollment.IntentInvite:
		// A federation-bound invite needs its identity snapshot first (unless
		// it is already there, mirroring the passkey /begin gate).
		if e.ExpectedUpstreamIdpSlug.Valid && e.ExpectedUpstreamIdpSlug.String != "" &&
			!(e.FederatedUpstreamIdpID.Valid && e.FederatedUpstreamIdpSlug.Valid && e.FederatedUpstreamIdpSlug.String == e.ExpectedUpstreamIdpSlug.String) {
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{Factor: audit.FactorEnrollment, Event: audit.EventFail, Detail: map[string]any{"reason": "federation_required"}})
			writeAuthErr(w, authn.WithFederationName(authn.ErrEnrollmentFederationRequired(), s.enabledFederationName(r.Context(), e.ExpectedUpstreamIdpSlug.String)))
			return
		}
		role := "user"
		if e.TemplateRole.Valid {
			role = e.TemplateRole.String
		}
		inviteBody := enrollBeginBody{Username: body.Username, DisplayName: body.DisplayName}
		if err := applyFixedInvitationUsername(&inviteBody, e); err != nil {
			writeAuthErr(w, err)
			return
		}
		if inviteBody.DisplayName == "" && e.FederatedDisplayName.Valid {
			inviteBody.DisplayName = e.FederatedDisplayName.String
		}
		_, proposal, err = prepareNewEnrollmentAccount(r.Context(), q, inviteBody, role, "enrollment/password-totp/verify invite")
		if err != nil {
			writeAuthErr(w, err)
			return
		}

	case enrollment.IntentReset:
		if body.Username != "" || body.DisplayName != "" {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		if !e.TargetAccountID.Valid {
			writeAuthErr(w, authn.ErrEnrollmentConsumed())
			return
		}
		a, err := q.GetAccountByID(r.Context(), e.TargetAccountID.Int32)
		if err != nil || a.Disabled {
			writeAuthErr(w, authn.ErrEnrollmentConsumed())
			return
		}

	default:
		writeAuthErr(w, authn.ErrEnrollmentConsumed())
		return
	}

	tx, err := s.beginEnrollmentTx(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify: begin tx: %w", err))
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	qtx := tx.Queries()

	consumed, err := enrollment.ConsumeEnrollment(r.Context(), qtx, token)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	// Belt-and-suspenders: reject bootstrap here too, in case a stash predates a
	// policy change. The tx rolls back the already-consumed enrollment.
	if consumed.Intent == enrollment.IntentBootstrap {
		writeAuthErr(w, authn.ErrEnrollmentMethodNotAllowed())
		return
	}

	var (
		acct              db.Account
		federatedProvider *federation.Provider
		federatedAvatar   string
	)

	switch consumed.Intent {
	case enrollment.IntentInvite:
		if consumed.ExpectedUpstreamIdpSlug.Valid && consumed.ExpectedUpstreamIdpSlug.String != "" &&
			!(consumed.FederatedUpstreamIdpID.Valid && consumed.FederatedUpstreamIdpSlug.Valid && consumed.FederatedUpstreamIdpSlug.String == consumed.ExpectedUpstreamIdpSlug.String) {
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				Factor: audit.FactorEnrollment,
				Event:  audit.EventFail,
				Detail: map[string]any{"reason": "federation_required"},
			})
			writeAuthErr(w, authn.WithFederationName(authn.ErrEnrollmentFederationRequired(), s.enabledFederationName(r.Context(), consumed.ExpectedUpstreamIdpSlug.String)))
			return
		}
		if proposal.Username == "" {
			writeAuthErr(w, authn.ErrCeremonyState())
			return
		}
		if consumed.TemplateUsername.Valid {
			proposal.Username = consumed.TemplateUsername.String
		}
		role := "user"
		if consumed.TemplateRole.Valid {
			role = consumed.TemplateRole.String
		}
		attrs := enrollment.DecodeTemplateAttributes(consumed.TemplateAttributes)
		a, aerr := qtx.InsertAccount(r.Context(), db.InsertAccountParams{
			Username:           proposal.Username,
			DisplayName:        proposal.DisplayName,
			WebauthnUserHandle: proposal.WebauthnUserHandle,
			Role:               role,
			Attributes:         encodeAttributes(attrs),
			Disabled:           false,
		})
		if aerr != nil {
			if isUniqueViolation(aerr) {
				writeAuthErr(w, authn.ErrUsernameTaken())
				return
			}
			writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify invite: insert account: %w", aerr))
			return
		}
		acct = a
		if consumed.FederatedUpstreamIdpID.Valid {
			// Same shared identity-write as the passkey path; any failure rolls
			// the consume back.
			if _, ierr := insertInviteIdentityTx(r.Context(), qtx, tx.AuditWriter(), acct.ID, *consumed); ierr != nil {
				writeAuthErr(w, ierr)
				return
			}
			provider, perr := recheckInviteIdentityProvider(r.Context(), qtx, *consumed)
			if perr != nil {
				writeAuthErr(w, perr)
				return
			}
			federatedProvider = &provider
			federatedAvatar = inviteSnapshotAvatar(*consumed)
		}

	case enrollment.IntentReset:
		if !consumed.TargetAccountID.Valid {
			writeAuthErr(w, authn.ErrEnrollmentConsumed())
			return
		}
		a, aerr := qtx.GetAccountByIDForUpdate(r.Context(), consumed.TargetAccountID.Int32)
		if aerr != nil {
			if errors.Is(aerr, pgx.ErrNoRows) {
				writeAuthErr(w, authn.ErrAccountNotFound())
				return
			}
			writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify reset: get account: %w", aerr))
			return
		}
		if a.Disabled {
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				AccountID: &a.ID,
				Factor:    audit.FactorEnrollment,
				Event:     audit.EventFail,
				Detail:    map[string]any{"reason": "account_disabled"},
			})
			writeAuthErr(w, authn.ErrEnrollmentConsumed())
			return
		}
		// Reset wipes ALL prior credentials (passkeys here; the password/TOTP/
		// recovery rows are replaced below by UpsertPasswordCredential +
		// EnrollConfirmedForTx).
		if derr := qtx.DeleteAllCredentialsForAccount(r.Context(), a.ID); derr != nil {
			writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify reset: delete creds: %w", derr))
			return
		}
		acct = a

	default:
		writeAuthErr(w, authn.ErrEnrollmentConsumed())
		return
	}

	// Insert the fallback factor set. No audit inside the tx — emitted
	// post-commit so credential_event reflects only persisted state and the FK
	// to a freshly-inserted account resolves on all connections.
	if perr := qtx.UpsertPasswordCredential(r.Context(), db.UpsertPasswordCredentialParams{
		AccountID: acct.ID,
		Hash:      passwordPHC,
	}); perr != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify: set password: %w", perr))
		return
	}
	recoveryCodes, terr := s.totpStore.EnrollConfirmedForTx(r.Context(), qtx, acct.ID, body.SecretBase32, matchedStep)
	if terr != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify: enroll totp: %w", terr))
		return
	}
	if consumed.Intent == enrollment.IntentInvite {
		if err := enrollment.ApplyInvitationGroups(r.Context(), qtx, consumed.GroupIds, acct.ID, consumed.CreatedByAccountID); err != nil {
			writeAuthErr(w, err)
			return
		}
	}

	if cerr := tx.Commit(r.Context()); cerr != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify: commit: %w", cerr))
		return
	}

	// Federated_register inherits the upstream avatar (mirror the passkey path).
	if federatedProvider != nil && federatedAvatar != "" && s.enrollmentAvatarRefresh != nil {
		s.enrollmentAvatarRefresh(r.Context(), acct.ID, *federatedProvider, s.federatedAvatarDelivery(*federatedProvider, federatedAvatar))
	}

	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event":      "auth.enrollment_consumed",
		"intent":     consumed.Intent,
		"method":     enrollMethodPasswordTOTP,
		"account_id": acct.ID,
		"client_ip":  s.clientIP.IP(r),
	}).Info("auth")

	// Post-commit audit (account row now visible → credential_event FK resolves).
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: &acct.ID, Factor: audit.FactorPassword, Event: audit.EventRegister})
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: &acct.ID, Factor: audit.FactorTOTP, Event: audit.EventRegister})
	for range recoveryCodes {
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: &acct.ID, Factor: audit.FactorRecoveryCode, Event: audit.EventRegister})
	}
	auditDetail := map[string]any{"intent": string(consumed.Intent), "method": enrollMethodPasswordTOTP}
	if consumed.Intent == enrollment.IntentInvite {
		auditDetail["fixed_username"] = consumed.TemplateUsername.Valid
		auditDetail["group_ids"] = append([]int32(nil), consumed.GroupIds...)
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: &acct.ID,
		Factor:    audit.FactorEnrollment,
		Event:     audit.EventEnrollmentConsumed,
		Detail:    auditDetail,
	})

	if consumed.Intent == enrollment.IntentReset {
		// Best-effort revocation: an admin-issued reset token is single-use
		// and consumed here; failing the response after commit would strand
		// the user without credentials or guidance.
		_, _ = s.sessionStore.RevokeAllForAccount(r.Context(), acct.ID)
	}

	if me := s.maintenanceLockout(r.Context(), acct.ID); me != nil {
		writeAuthErr(w, me)
		return
	}
	ip := s.clientIP.IP(r)
	sessionToken, data, err := s.sessionStore.Issue(r.Context(), acct.ID, ip, r.UserAgent(), []string{"pwd", "otp", "mfa"}, nil, false)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify: session issue: %w", err))
		return
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: &acct.ID,
		Factor:    audit.FactorSession,
		Event:     audit.EventSessionStart,
		Detail:    map[string]any{"via": "enrollment"},
	})
	http.SetCookie(w, s.sessionStore.SessionCookie(s.config, r, acct.ID, sessionToken, data))

	type resp struct {
		Session       contract.SessionView `json:"session"`
		RecoveryCodes []string             `json:"recoveryCodes"`
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp{
		Session:       s.sessionView(&acct),
		RecoveryCodes: recoveryCodes,
	})
}
