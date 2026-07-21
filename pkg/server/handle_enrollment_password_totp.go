// Package server — handle_enrollment_password_totp.go
//
// The password+TOTP enrollment ceremony: the non-passkey way to redeem an
// enrollment token. Every intent EXCEPT bootstrap (the first-admin CLI) may
// use it — invite (user or admin role), federated_register (VRChat), and reset
// (incl. VRChat provider-recovery). The passkey ceremony
// (handle_enrollment.go) stays available for all intents; this file adds a
// parallel path, it does not replace anything.
//
// Because a password is only a usable login factor when paired with a
// confirmed TOTP (authn.AvailableMethods), this path always creates
// password + confirmed TOTP + 10 recovery codes together and issues a session
// with amr=["pwd","otp","mfa"].
//
// Shape mirrors the login-time recovery ceremony (handle_auth_recovery.go):
//
//	POST /enrollments/{token}/password-totp/begin  {username, displayName, password}
//	  → 200 {secret_base32, otpauth_uri}
//	  Hashes the password, generates a TOTP secret (NO DB write — the account
//	  may not exist yet), and stashes both in KV keyed by the token.
//
//	POST /enrollments/{token}/password-totp/verify {code}
//	  → 200 {session, recoveryCodes:[...]} + session cookie
//	  Verifies the code against the stashed secret, then in ONE tx: creates
//	  (or resets) the account, inserts password + confirmed TOTP + recovery
//	  codes, consumes the enrollment. Audit + session issue happen post-commit.

package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

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
	sessstore "prohibitorum/pkg/session"
)

const (
	enrollMethodPasskey      = "passkey"
	enrollMethodPasswordTOTP = "password_totp"
)

// enrollPwdTOTPCeremonyTTL bounds the scan-QR-then-type-code window. Longer
// than the passkey enroll stash (5m) since typing a TOTP code from an
// authenticator app is slower; shorter would be user-hostile.
const enrollPwdTOTPCeremonyTTL = 10 * time.Minute

// enrollmentAllowedMethods derives the method policy from the intent alone:
// only the first-admin bootstrap is passkey-only; every other intent offers
// passkey OR password+TOTP. No role lookup or schema column is needed.
func enrollmentAllowedMethods(intent string) []string {
	if intent == enrollment.IntentBootstrap {
		return []string{enrollMethodPasskey}
	}
	return []string{enrollMethodPasskey, enrollMethodPasswordTOTP}
}

// enrollPwdTOTPCeremonyKey derives the KV key from a SHA-256 of the token so the
// bearer secret never lands in the keyspace (matches enrollCeremonyKey). The
// distinct prefix lets a user hold a passkey ceremony and a password+TOTP
// ceremony for the same token without collision.
func enrollPwdTOTPCeremonyKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "enroll_pwdtotp:" + hex.EncodeToString(sum[:])
}

// enrollPwdTOTPStash is the KV payload between begin and verify. For creation
// intents it carries the pending account identity; for reset only PasswordPHC +
// TOTPSecretBase32 matter (the target is loaded fresh in the tx). The password
// is an argon2id PHC (never plaintext); the raw base32 TOTP secret was already
// returned to the client for the QR, and is encrypted only once account_id
// exists at verify.
type enrollPwdTOTPStash struct {
	Username           string `json:"username,omitempty"`
	DisplayName        string `json:"displayName,omitempty"`
	WebauthnUserHandle []byte `json:"webauthn_user_handle,omitempty"`
	PasswordPHC        string `json:"password_phc"`
	TOTPSecretBase32   string `json:"totp_secret_base32"`
}

// ----- begin ---------------------------------------------------------------

func (s *Server) handleEnrollmentPasswordTOTPBeginHTTP(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	q := s.enrollmentQ()
	e, err := enrollment.LoadEnrollment(r.Context(), q, token)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	// Policy: bootstrap is passkey-only.
	if e.Intent == enrollment.IntentBootstrap {
		writeAuthErr(w, authn.ErrEnrollmentMethodNotAllowed())
		return
	}

	var body struct {
		Username    string `json:"username,omitempty"`
		DisplayName string `json:"displayName,omitempty"`
		Password    string `json:"password"`
	}
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	// OWASP 2026 §5.1.1.2 bounds — mirror handleMePasswordSetHTTP.
	if len(body.Password) < 8 || len(body.Password) > 1024 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	var (
		stash     enrollPwdTOTPStash
		totpLabel string
	)
	switch e.Intent {
	case enrollment.IntentInvite:
		// Federation-bound invites MUST redeem via start-federation, not a
		// local credential (mirror the passkey begin gate).
		if e.ExpectedUpstreamIdpSlug.Valid && e.ExpectedUpstreamIdpSlug.String != "" {
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				Factor: audit.FactorEnrollment,
				Event:  audit.EventFail,
				Detail: map[string]any{"reason": "federation_required"},
			})
			writeAuthErr(w, authn.ErrEnrollmentFederationRequired())
			return
		}
		role := "user"
		if e.TemplateRole.Valid {
			role = e.TemplateRole.String
		}
		_, proposal, perr := prepareNewEnrollmentAccount(r.Context(), q, enrollBeginBody{Username: body.Username, DisplayName: body.DisplayName}, role, "enrollment/password-totp/begin invite")
		if perr != nil {
			writeAuthErr(w, perr)
			return
		}
		stash.Username, stash.DisplayName, stash.WebauthnUserHandle = proposal.Username, proposal.DisplayName, proposal.WebauthnUserHandle
		totpLabel = proposal.Username

	case enrollment.IntentFederatedRegister:
		if !e.FederatedUpstreamIdpID.Valid {
			writeAuthErr(w, authn.ErrProviderNotReady())
			return
		}
		if _, perr := s.recheckVRChatEnrollmentProvider(r.Context(), q, e.FederatedUpstreamIdpID.Int64); perr != nil {
			writeAuthErr(w, perr)
			return
		}
		_, proposal, perr := prepareNewEnrollmentAccount(r.Context(), q, enrollBeginBody{Username: body.Username, DisplayName: body.DisplayName}, "user", "enrollment/password-totp/begin federated")
		if perr != nil {
			writeAuthErr(w, perr)
			return
		}
		stash.Username, stash.DisplayName, stash.WebauthnUserHandle = proposal.Username, proposal.DisplayName, proposal.WebauthnUserHandle
		totpLabel = proposal.Username

	case enrollment.IntentReset:
		if !e.TargetAccountID.Valid {
			writeAuthErr(w, authn.ErrEnrollmentConsumed())
			return
		}
		providerRecovery := e.RecoverySourceUpstreamIdpID.Valid
		if providerRecovery {
			if _, perr := s.recheckVRChatEnrollmentProvider(r.Context(), q, e.RecoverySourceUpstreamIdpID.Int64); perr != nil {
				writeAuthErr(w, perr)
				return
			}
		}
		a, gerr := q.GetAccountByID(r.Context(), e.TargetAccountID.Int32)
		if gerr != nil {
			writeAuthErr(w, authn.ErrEnrollmentConsumed())
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
		// Provider recovery keeps the otpauth label neutral (mirrors the
		// passkey path's neutral WebAuthn labels for public recovery).
		if providerRecovery {
			totpLabel = "account"
		} else {
			totpLabel = a.Username
		}

	default:
		writeAuthErr(w, authn.ErrEnrollmentConsumed())
		return
	}

	// Hash the password now — the KV stash holds only the PHC, never plaintext.
	phc, herr := s.passwordStore.Hash(body.Password)
	if herr != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/password-totp/begin: hash: %w", herr))
		return
	}
	stash.PasswordPHC = phc

	// Generate the TOTP secret now — no DB write (account may not exist yet).
	enr, gerr := s.totpStore.GenerateEnrollment(totpLabel)
	if gerr != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/password-totp/begin: totp: %w", gerr))
		return
	}
	stash.TOTPSecretBase32 = enr.SecretBase32

	raw, merr := json.Marshal(stash)
	if merr != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/password-totp/begin: marshal: %w", merr))
		return
	}
	if serr := s.kvStore.SetEx(r.Context(), enrollPwdTOTPCeremonyKey(token), string(raw), enrollPwdTOTPCeremonyTTL); serr != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/password-totp/begin: setex: %w", serr))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"secret_base32": enr.SecretBase32,
		"otpauth_uri":   enr.ProvisioningURI,
	})
}

// ----- verify --------------------------------------------------------------

func (s *Server) handleEnrollmentPasswordTOTPVerifyHTTP(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	q := s.enrollmentQ()
	if _, err := enrollment.LoadEnrollment(r.Context(), q, token); err != nil {
		writeAuthErr(w, err)
		return
	}

	// Non-destructive Get: a wrong code is retryable (the enrollment token, not
	// the stash, is the single-use anchor — it's only consumed on success).
	raw, err := s.kvStore.Get(r.Context(), enrollPwdTOTPCeremonyKey(token))
	if err != nil {
		writeAuthErr(w, authn.ErrCeremonyExpired())
		return
	}
	var stash enrollPwdTOTPStash
	if err := json.Unmarshal([]byte(raw), &stash); err != nil {
		writeAuthErr(w, authn.ErrCeremonyState())
		return
	}

	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}
	matchedStep, ok := s.totpStore.VerifyCandidateSecret(stash.TOTPSecretBase32, body.Code)
	if !ok {
		writeAuthErr(w, authn.ErrBadCredentials())
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
		if consumed.ExpectedUpstreamIdpSlug.Valid && consumed.ExpectedUpstreamIdpSlug.String != "" {
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				Factor: audit.FactorEnrollment,
				Event:  audit.EventFail,
				Detail: map[string]any{"reason": "federation_required"},
			})
			writeAuthErr(w, authn.ErrEnrollmentFederationRequired())
			return
		}
		if stash.Username == "" {
			writeAuthErr(w, authn.ErrCeremonyState())
			return
		}
		role := "user"
		if consumed.TemplateRole.Valid {
			role = consumed.TemplateRole.String
		}
		attrs := enrollment.DecodeTemplateAttributes(consumed.TemplateAttributes)
		a, aerr := qtx.InsertAccount(r.Context(), db.InsertAccountParams{
			Username:           stash.Username,
			DisplayName:        stash.DisplayName,
			WebauthnUserHandle: stash.WebauthnUserHandle,
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

	case enrollment.IntentFederatedRegister:
		if stash.Username == "" {
			writeAuthErr(w, authn.ErrCeremonyState())
			return
		}
		if !consumed.FederatedUpstreamIdpID.Valid || !consumed.FederatedUpstreamIss.Valid ||
			!consumed.FederatedUpstreamSub.Valid || len(consumed.FederatedUpstreamData) == 0 {
			writeAuthErr(w, authn.ErrCeremonyState())
			return
		}
		provider, perr := s.recheckVRChatEnrollmentProvider(r.Context(), qtx, consumed.FederatedUpstreamIdpID.Int64)
		if perr != nil {
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				Factor: audit.FactorEnrollment,
				Event:  audit.EventFail,
				Detail: map[string]any{"reason": "provider_unavailable"},
			})
			writeAuthErr(w, perr)
			return
		}
		a, aerr := qtx.InsertAccount(r.Context(), db.InsertAccountParams{
			Username:           stash.Username,
			DisplayName:        stash.DisplayName,
			WebauthnUserHandle: stash.WebauthnUserHandle,
			Role:               "user",
			Attributes:         []byte("{}"),
			Disabled:           false,
		})
		if aerr != nil {
			if isUniqueViolation(aerr) {
				audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
					Factor: audit.FactorEnrollment,
					Event:  audit.EventFail,
					Detail: map[string]any{"reason": "username_collision"},
				})
				writeAuthErr(w, authn.ErrUsernameTaken())
				return
			}
			writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify federated: insert account: %w", aerr))
			return
		}
		acct = a
		if _, ierr := qtx.GetAccountIdentityByIssuerSub(r.Context(), db.GetAccountIdentityByIssuerSubParams{
			UpstreamIss: consumed.FederatedUpstreamIss.String,
			UpstreamSub: consumed.FederatedUpstreamSub.String,
		}); ierr == nil {
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				Factor: audit.FactorEnrollment,
				Event:  audit.EventFail,
				Detail: map[string]any{"reason": "identity_conflict"},
			})
			writeAuthErr(w, authn.ErrFederationIdentityConflict())
			return
		} else if !errors.Is(ierr, pgx.ErrNoRows) {
			writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify federated: check identity: %w", ierr))
			return
		}
		identity, ierr := qtx.InsertAccountIdentity(r.Context(), db.InsertAccountIdentityParams{
			AccountID:     a.ID,
			UpstreamIdpID: provider.ID,
			UpstreamIss:   consumed.FederatedUpstreamIss.String,
			UpstreamSub:   consumed.FederatedUpstreamSub.String,
			UpstreamData:  append([]byte(nil), consumed.FederatedUpstreamData...),
		})
		if ierr != nil {
			if isUniqueViolation(ierr) {
				audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
					Factor: audit.FactorEnrollment,
					Event:  audit.EventFail,
					Detail: map[string]any{"reason": "identity_conflict"},
				})
				writeAuthErr(w, authn.ErrFederationIdentityConflict())
				return
			}
			writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify federated: insert identity: %w", ierr))
			return
		}
		if cerr := qtx.ConfirmAccountIdentity(r.Context(), identity.ID); cerr != nil {
			writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify federated: confirm identity: %w", cerr))
			return
		}
		federatedProvider = &provider
		if consumed.FederatedAvatarUrl.Valid {
			federatedAvatar = consumed.FederatedAvatarUrl.String
		}

	case enrollment.IntentReset:
		if consumed.RecoverySourceUpstreamIdpID.Valid {
			if _, perr := s.recheckVRChatEnrollmentProvider(r.Context(), qtx, consumed.RecoverySourceUpstreamIdpID.Int64); perr != nil {
				audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
					Factor: audit.FactorEnrollment,
					Event:  audit.EventFail,
					Detail: map[string]any{"reason": "provider_unavailable"},
				})
				writeAuthErr(w, perr)
				return
			}
		}
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
		Hash:      stash.PasswordPHC,
	}); perr != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify: set password: %w", perr))
		return
	}
	recoveryCodes, terr := s.totpStore.EnrollConfirmedForTx(r.Context(), qtx, acct.ID, stash.TOTPSecretBase32, matchedStep)
	if terr != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify: enroll totp: %w", terr))
		return
	}

	if cerr := tx.Commit(r.Context()); cerr != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify: commit: %w", cerr))
		return
	}

	// Federated_register inherits the upstream avatar (mirror the passkey path).
	if federatedProvider != nil && federatedAvatar != "" && s.enrollmentAvatarOverride != nil {
		_ = s.enrollmentAvatarOverride(acct.ID, *federatedProvider, federation.AvatarDelivery{URL: federatedAvatar})
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
	if consumed.Intent == enrollment.IntentReset && consumed.RecoverySourceUpstreamIdpID.Valid {
		auditDetail["source"] = "vrchat"
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: &acct.ID,
		Factor:    audit.FactorEnrollment,
		Event:     audit.EventEnrollmentConsumed,
		Detail:    auditDetail,
	})

	// Best-effort stash cleanup.
	_ = s.kvStore.Del(r.Context(), enrollPwdTOTPCeremonyKey(token))
	if consumed.Intent == enrollment.IntentReset {
		_, revokeErr := s.sessionStore.RevokeAllForAccount(r.Context(), acct.ID)
		if revokeErr != nil && consumed.RecoverySourceUpstreamIdpID.Valid {
			// Credential replacement + enrollment consumption already committed.
			// Fail closed rather than issue a fresh session while a compromised
			// old session may still be live.
			writeAuthErr(w, fmt.Errorf("enrollment/password-totp/verify: revoke sessions: %w", revokeErr))
			return
		}
	}

	if me := s.maintenanceLockout(r.Context(), acct.ID); me != nil {
		writeAuthErr(w, me)
		return
	}
	ip := s.clientIP.IP(r)
	sessionToken, _, err := s.sessionStore.Issue(r.Context(), acct.ID, ip, r.UserAgent(), []string{"pwd", "otp", "mfa"}, nil)
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
	http.SetCookie(w, sessstore.FreshSessionCookie(s.config, r, acct.ID, sessionToken, s.config.SessionTTL))

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
