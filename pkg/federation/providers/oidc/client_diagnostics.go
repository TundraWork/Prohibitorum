package oidc

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/zitadel/oidc/v3/pkg/client/rp"
	oidclib "github.com/zitadel/oidc/v3/pkg/oidc"
	"golang.org/x/oauth2"
)

type diagnosticTransport struct {
	base     http.RoundTripper
	endpoint string
	status   int
	duration time.Duration
}

func (t *diagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	begin := time.Now()
	response, err := t.base.RoundTrip(r)
	if r.URL.String() == t.endpoint {
		t.duration += time.Since(begin)
		if response != nil {
			t.status = response.StatusCode
		}
	}
	return response, err
}

type observedRelyingParty struct {
	rp.RelyingParty
	httpClient *http.Client
}

func (r observedRelyingParty) HttpClient() *http.Client { return r.httpClient }

// ExchangeDiagnostic observes only status/timing around the same Exchange path
// used by real login. It never captures a request or response body.
func (c *Client) ExchangeDiagnostic(ctx context.Context, code, verifier, issuer, nonce, requestID string) (*Tokens, []DiagnosticStage, error) {
	observed := *c.rp.HttpClient()
	transport := observed.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	recorder := &diagnosticTransport{base: transport, endpoint: c.TokenEndpoint()}
	observed.Transport = recorder
	client := *c
	client.rp = observedRelyingParty{RelyingParty: c.rp, httpClient: &observed}
	begin := time.Now()
	tokens, err := client.Exchange(ctx, code, verifier, issuer, nonce)
	exchange := DiagnosticStage{Name: "token_exchange", Status: "succeeded", Endpoint: DiagnosticEndpoint(c.TokenEndpoint()), HTTPStatus: recorder.status, DurationMS: recorder.duration.Milliseconds(), RequestID: requestID}
	verification := DiagnosticStage{Name: "id_token", Status: "succeeded", DurationMS: (time.Since(begin) - recorder.duration).Milliseconds(), RequestID: requestID}
	if err != nil {
		var retrieval *oauth2.RetrieveError
		if errors.As(err, &retrieval) || recorder.status < 200 || recorder.status >= 300 {
			exchange.Status = "failed"
			exchange.ErrorCode = "token_exchange_failed"
			if retrieval != nil {
				exchange.ErrorCode = safeOAuthError(retrieval.ErrorCode)
			}
			verification.Status = "skipped"
		} else {
			verification.Status = "failed"
			verification.ErrorCode = diagnosticVerificationError(err)
		}
	}
	return tokens, []DiagnosticStage{exchange, verification}, err
}

func (c *Client) UserInfoDiagnostic(ctx context.Context, accessToken, subject string) (map[string]any, int, error) {
	observed := *c.rp.HttpClient()
	transport := observed.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	recorder := &diagnosticTransport{base: transport, endpoint: c.rp.UserinfoEndpoint()}
	observed.Transport = recorder
	client := *c
	client.rp = observedRelyingParty{RelyingParty: c.rp, httpClient: &observed}
	info, err := client.UserInfo(ctx, accessToken, subject)
	return info, recorder.status, err
}

func diagnosticVerificationError(err error) string {
	for _, entry := range []struct {
		cause error
		code  string
	}{
		{oidclib.ErrIssuerInvalid, "issuer_mismatch"}, {oidclib.ErrAudience, "audience_mismatch"}, {oidclib.ErrNonceInvalid, "nonce_mismatch"}, {oidclib.ErrExpired, "token_expired"}, {oidclib.ErrSignatureUnsupportedAlg, "signing_algorithm"}, {oidclib.ErrSignatureInvalid, "signature_invalid"}, {oidclib.ErrAtHash, "access_token_hash"}, {rp.ErrMissingIDToken, "missing_id_token"},
	} {
		if errors.Is(err, entry.cause) {
			return entry.code
		}
	}
	return "id_token_invalid"
}
