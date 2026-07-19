package server

import (
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/federation"
	sessstore "prohibitorum/pkg/session"
)

type federationFlowView struct {
	Provider struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"displayName"`
		Protocol    string `json:"protocol"`
	} `json:"provider"`
	Intent                string    `json:"intent"`
	Step                  string    `json:"step"`
	ProfileURL            string    `json:"profileUrl,omitempty"`
	ProofURL              string    `json:"proofUrl,omitempty"`
	RequiresLocalUsername bool      `json:"requiresLocalUsername"`
	ExpiresAt             time.Time `json:"expiresAt"`
}

type federationFlowPrepareBody struct {
	Identity string `json:"identity"`
}

type federationFlowVerifyBody struct {
	LocalUsername string `json:"localUsername,omitempty"`
}

func (s *Server) handleFederationFlowGetHTTP(w http.ResponseWriter, r *http.Request) {
	setFederationFlowHeaders(w)
	view, err := s.readFederationFlow(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	writeJSON(w, view)
}

func (s *Server) handleFederationFlowPrepareHTTP(w http.ResponseWriter, r *http.Request) {
	setFederationFlowHeaders(w)
	var body federationFlowPrepareBody
	if err := decodeStrictOperatorJSON(r, &body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	request := s.localFederationAdvanceRequest(r)
	request.Input = federation.ActionInput{Kind: federation.ActionCollectIdentity, Identity: body.Identity}
	view, err := s.federationService.PrepareFlow(r.Context(), request)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	wire, err := projectFederationFlow(view)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	writeJSON(w, wire)
}

func (s *Server) handleFederationFlowVerifyHTTP(w http.ResponseWriter, r *http.Request) {
	setFederationFlowHeaders(w)
	var body federationFlowVerifyBody
	if r.ContentLength != 0 {
		if err := decodeStrictOperatorJSON(r, &body); err != nil {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
	}
	request := s.localFederationAdvanceRequest(r)
	request.Input = federation.ActionInput{Kind: federation.ActionPublishProof, LocalUsername: body.LocalUsername}
	result, err := s.federationService.VerifyFlow(r.Context(), request)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	s.writeFederationCompletion(w, r, result, federationCompletionJSON)
}

func (s *Server) readFederationFlow(r *http.Request) (*federationFlowView, error) {
	request := s.localFederationAdvanceRequest(r)
	view, err := s.federationService.ReadFlow(r.Context(), federation.FlowReadRequest{
		FlowID: request.FlowID, BrowserToken: request.BrowserToken, CallbackRoute: request.CallbackRoute,
		AccountID: request.AccountID, SessionID: request.SessionID,
	})
	if err != nil {
		return nil, err
	}
	return projectFederationFlow(view)
}

func (s *Server) localFederationAdvanceRequest(r *http.Request) federation.AdvanceRequest {
	request := federation.AdvanceRequest{
		FlowID: chi.URLParam(r, "flow"), CallbackRoute: federation.CallbackRouteLocal,
	}
	if cookie, err := r.Cookie(sessstore.FedStateCookieName); err == nil {
		request.BrowserToken = cookie.Value
	}
	if session := authn.SessionFromContext(r.Context()); session != nil && session.Account != nil && session.Data != nil {
		request.AccountID = new(session.Account.ID)
		request.SessionID = session.Data.SessionID
	}
	return request
}

func projectFederationFlow(source *federation.FlowView) (*federationFlowView, error) {
	if source == nil {
		return nil, authn.ErrFederationStateInvalid()
	}
	view := &federationFlowView{
		Intent: string(source.Intent), ExpiresAt: source.ExpiresAt,
	}
	view.Provider.Slug = source.ProviderSlug
	view.Provider.DisplayName = source.ProviderDisplayName
	view.Provider.Protocol = source.Protocol
	switch source.Action.Kind {
	case federation.ActionCollectIdentity:
		view.Step = "identify"
	case federation.ActionPublishProof:
		view.Step = "proof"
		var ok bool
		if view.ProfileURL, ok = source.Action.Public["profileUrl"].(string); !ok || view.ProfileURL == "" {
			return nil, authn.ErrFederationStateInvalid()
		}
		if view.ProofURL, ok = source.Action.Public["proofUrl"].(string); !ok || view.ProofURL == "" {
			return nil, authn.ErrFederationStateInvalid()
		}
		if required, exists := source.Action.Public["requiresLocalUsername"]; exists {
			if view.RequiresLocalUsername, ok = required.(bool); !ok {
				return nil, authn.ErrFederationStateInvalid()
			}
		}
	default:
		return nil, authn.ErrFederationActionInvalid()
	}
	return view, nil
}

func federationBeginDestination(begin *federation.BeginResult) (string, error) {
	if begin == nil {
		return "", authn.ErrFederationStateInvalid()
	}
	switch begin.Action.Kind {
	case federation.ActionRedirect:
		if begin.Action.URL == "" {
			return "", authn.ErrFederationStateInvalid()
		}
		return begin.Action.URL, nil
	case federation.ActionCollectIdentity:
		return "/federation/flow/" + url.PathEscape(begin.FlowID), nil
	default:
		return "", authn.ErrFederationActionInvalid()
	}
}

func withFederationFlowBodyControls(handler http.HandlerFunc) http.HandlerFunc {
	controlled := withAdminBodyControls(handler)
	return func(w http.ResponseWriter, r *http.Request) {
		setFederationFlowHeaders(w)
		controlled(w, r)
	}
}

func setFederationFlowHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}
