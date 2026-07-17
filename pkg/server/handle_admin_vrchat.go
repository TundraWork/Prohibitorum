package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/federation/providers/vrchat"
)

const (
	vrchatOperatorSetupStarted       = "vrchat_operator_setup_started"
	vrchatOperatorChallengeIssued    = "vrchat_operator_challenge_issued"
	vrchatOperatorSessionValidated   = "vrchat_operator_session_validated"
	vrchatOperatorSessionInvalidated = "vrchat_operator_session_invalidated"
)

type vrchatOperatorService interface {
	Start(context.Context, string, int32, string, string, string) (vrchat.OperatorSessionResult, error)
	Verify(context.Context, string, int32, string, string, string, string) (vrchat.OperatorSessionResult, error)
	Validate(context.Context, string) (vrchat.OperatorSessionResult, error)
}

type OperatorSessionResult struct {
	Status    string                         `json:"status"`
	Challenge string                         `json:"challenge,omitempty"`
	Methods   []string                       `json:"methods,omitempty"`
	ExpiresAt *time.Time                     `json:"expiresAt,omitempty"`
	Provider  *contract.IdentityProviderView `json:"provider,omitempty"`
}
type operatorStartBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type operatorVerifyBody struct {
	Challenge string `json:"challenge"`
	Method    string `json:"method"`
	Code      string `json:"code"`
}

func (s *Server) vrchatOperator() vrchatOperatorService {
	if s.vrchatOperatorOverride != nil {
		return s.vrchatOperatorOverride
	}
	return s.vrchatOperatorService
}

func (s *Server) handleVRChatOperatorStartHTTP(w http.ResponseWriter, r *http.Request) {
	var body operatorStartBody
	if err := decodeStrictOperatorJSON(r, &body); err != nil || body.Username == "" || body.Password == "" {
		s.finishVRChatOperator(w, r, "start", "", nil, authn.ErrBadRequest())
		return
	}
	session := authn.SessionFromContext(r.Context())
	if session == nil || session.Data == nil {
		s.finishVRChatOperator(w, r, "start", "", nil, authn.ErrNoSession())
		return
	}
	service := s.vrchatOperator()
	if service == nil {
		s.finishVRChatOperator(w, r, "start", "", nil, errors.New("operator service unavailable"))
		return
	}
	result, err := service.Start(r.Context(), chi.URLParam(r, "slug"), session.Data.AccountID, session.Data.SessionID, body.Username, body.Password)
	s.finishVRChatOperator(w, r, "start", "", &result, err)
}
func (s *Server) handleVRChatOperatorVerifyHTTP(w http.ResponseWriter, r *http.Request) {
	var body operatorVerifyBody
	if err := decodeStrictOperatorJSON(r, &body); err != nil || body.Challenge == "" || body.Method == "" || body.Code == "" {
		s.finishVRChatOperator(w, r, "verify", body.Method, nil, authn.ErrBadRequest())
		return
	}
	session := authn.SessionFromContext(r.Context())
	if session == nil || session.Data == nil {
		s.finishVRChatOperator(w, r, "verify", body.Method, nil, authn.ErrNoSession())
		return
	}
	service := s.vrchatOperator()
	if service == nil {
		s.finishVRChatOperator(w, r, "verify", body.Method, nil, errors.New("operator service unavailable"))
		return
	}
	result, err := service.Verify(r.Context(), chi.URLParam(r, "slug"), session.Data.AccountID, session.Data.SessionID, body.Challenge, body.Method, body.Code)
	s.finishVRChatOperator(w, r, "verify", body.Method, &result, err)
}
func (s *Server) handleVRChatOperatorValidateHTTP(w http.ResponseWriter, r *http.Request) {
	var one [1]byte
	if n, err := r.Body.Read(one[:]); n != 0 || err != io.EOF {
		s.finishVRChatOperator(w, r, "validate", "", nil, authn.ErrBadRequest())
		return
	}
	service := s.vrchatOperator()
	if service == nil {
		s.finishVRChatOperator(w, r, "validate", "", nil, errors.New("operator service unavailable"))
		return
	}
	result, err := service.Validate(r.Context(), chi.URLParam(r, "slug"))
	s.finishVRChatOperator(w, r, "validate", "", &result, err)
}

func decodeStrictOperatorJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func (s *Server) finishVRChatOperator(w http.ResponseWriter, r *http.Request, action, method string, result *vrchat.OperatorSessionResult, err error) {
	method = safeVRChatAuditMethod(method)
	slug := chi.URLParam(r, "slug")
	detail := map[string]any{"slug": slug, "action": action}
	if method != "" {
		detail["method"] = method
	}
	if err != nil {
		detail["category"] = vrchatOperatorAuditCategory(err)
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: sessionAccountID(r.Context()), Factor: audit.FactorUpstreamIDP, Event: vrchatOperatorSetupStarted, Detail: detail})
	if err != nil {
		if op := vrchat.AsOperatorError(err); action == "validate" && op != nil && op.SessionInvalidated {
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: sessionAccountID(r.Context()), Factor: audit.FactorUpstreamIDP, Event: vrchatOperatorSessionInvalidated, Detail: map[string]any{"slug": slug, "action": action, "category": string(vrchat.OperatorCategoryCredentialsInvalid)}})
		}
		writeVRChatOperatorError(w, err)
		return
	}
	wire, conversionErr := s.operatorWireResult(result)
	if conversionErr != nil {
		writeAuthErr(w, conversionErr)
		return
	}
	if wire.Status == vrchat.OperatorStatusChallenge {
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: sessionAccountID(r.Context()), Factor: audit.FactorUpstreamIDP, Event: vrchatOperatorChallengeIssued, Detail: map[string]any{"slug": slug, "action": action, "methods": append([]string(nil), wire.Methods...)}})
	} else {
		validated := map[string]any{"slug": slug, "action": action}
		if method != "" {
			validated["method"] = method
		}
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: sessionAccountID(r.Context()), Factor: audit.FactorUpstreamIDP, Event: vrchatOperatorSessionValidated, Detail: validated})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(wire)
}

func safeVRChatAuditMethod(method string) string {
	switch method {
	case "totp", "emailOtp", "otp":
		return method
	default:
		return ""
	}
}
func (s *Server) operatorWireResult(result *vrchat.OperatorSessionResult) (OperatorSessionResult, error) {
	if result == nil {
		return OperatorSessionResult{}, errors.New("missing operator result")
	}
	switch result.Status {
	case vrchat.OperatorStatusChallenge:
		if result.Challenge == "" || len(result.Methods) == 0 || result.ExpiresAt == nil || result.Provider != nil {
			return OperatorSessionResult{}, errors.New("invalid operator challenge result")
		}
		return OperatorSessionResult{Status: result.Status, Challenge: result.Challenge, Methods: append([]string(nil), result.Methods...), ExpiresAt: result.ExpiresAt}, nil
	case vrchat.OperatorStatusValid:
		if result.Provider == nil || result.Challenge != "" || len(result.Methods) != 0 || result.ExpiresAt != nil {
			return OperatorSessionResult{}, errors.New("invalid operator valid result")
		}
		view, err := s.identityProviderView(*result.Provider)
		if err != nil {
			return OperatorSessionResult{}, err
		}
		return OperatorSessionResult{Status: result.Status, Provider: &view}, nil
	default:
		return OperatorSessionResult{}, errors.New("invalid operator result status")
	}
}
func writeVRChatOperatorError(w http.ResponseWriter, err error) {
	op := vrchat.AsOperatorError(err)
	if op == nil {
		writeAuthErr(w, err)
		return
	}
	switch op.Category {
	case vrchat.OperatorCategoryCredentialsInvalid:
		writeAuthErr(w, authn.ErrVRChatOperatorCredentialsInvalid())
	case vrchat.OperatorCategoryChallengeInvalid:
		writeAuthErr(w, authn.ErrVRChatOperatorChallengeInvalid())
	case vrchat.OperatorCategoryCodeInvalid:
		writeAuthErr(w, authn.ErrVRChatOperatorCodeInvalid())
	case vrchat.OperatorCategoryUpstreamTemporarilyUnavailable:
		writeAuthErr(w, authn.ErrUpstreamTemporarilyUnavailable())
	case vrchat.OperatorCategoryUpstreamRateLimited:
		writeAuthErr(w, authn.ErrUpstreamRateLimited(op.RetryAfter))
	case vrchat.OperatorCategoryProviderNotReady:
		writeAuthErr(w, authn.ErrProviderNotReady())
	case vrchat.OperatorCategoryBadRequest:
		writeAuthErr(w, authn.ErrBadRequest())
	case vrchat.OperatorCategoryDatabaseUnavailable:
		writeAuthErrForCode(w, "database_unavailable", err)
	case vrchat.OperatorCategoryKVUnavailable:
		writeAuthErrForCode(w, "kv_unavailable", err)
	default:
		writeAuthErr(w, errors.New("operator service failure"))
	}
}
func vrchatOperatorAuditCategory(err error) string {
	if op := vrchat.AsOperatorError(err); op != nil {
		return string(op.Category)
	}
	if ae := authn.AsAuthError(err); ae != nil {
		return ae.Code
	}
	return string(vrchat.OperatorCategoryServerError)
}
