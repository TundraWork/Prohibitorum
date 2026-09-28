package oidc

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// diagnosticDocumentLimit bounds each document an OIDC connection test keeps,
// measured on compacted JSON.
const diagnosticDocumentLimit = 64 << 10

type upstreamEndpoint int

const (
	upstreamToken upstreamEndpoint = iota + 1
	upstreamUserInfo
	upstreamJWKS
)

// upstreamCall is the last observed request to one endpoint.
type upstreamCall struct {
	count    int
	status   int
	duration time.Duration
	end      time.Time
}

// diagnosticRecorder listens in on a login-path Adapter call on behalf of the
// administrator connection test. It lives on the context for the length of
// one Complete and is never persisted. Real login carries no recorder, and
// every method is a no-op on a nil receiver.
type diagnosticRecorder struct {
	mu          sync.Mutex
	calls       map[upstreamEndpoint]upstreamCall
	userInfo    compactCopy
	client      *Client
	tokens      *Tokens
	exchangedAt time.Time
}

type recorderCtxKey struct{}

func withRecorder(ctx context.Context) (context.Context, *diagnosticRecorder) {
	recorder := &diagnosticRecorder{calls: map[upstreamEndpoint]upstreamCall{}}
	return context.WithValue(ctx, recorderCtxKey{}, recorder), recorder
}

func recorderFrom(ctx context.Context) *diagnosticRecorder {
	recorder, _ := ctx.Value(recorderCtxKey{}).(*diagnosticRecorder)
	return recorder
}

func (r *diagnosticRecorder) request(endpoint upstreamEndpoint, response *http.Response, duration time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	call := r.calls[endpoint]
	call.count++
	call.status = 0
	if response != nil {
		call.status = response.StatusCode
	}
	call.duration = duration
	call.end = time.Now()
	r.calls[endpoint] = call
	if endpoint == upstreamUserInfo {
		r.userInfo = compactCopy{}
	}
}

func (r *diagnosticRecorder) call(endpoint upstreamEndpoint) upstreamCall {
	if r == nil {
		return upstreamCall{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[endpoint]
}

// exchanged receives the result of a successful Exchange: a verified id_token,
// or the no-id_token fallback tokens. The access token stays in memory so the
// test can make the same userinfo request login would.
func (r *diagnosticRecorder) exchanged(client *Client, tokens *Tokens) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.client, r.tokens, r.exchangedAt = client, tokens, time.Now()
}

func (r *diagnosticRecorder) exchange() (*Client, *Tokens, time.Time) {
	if r == nil {
		return nil, nil, time.Time{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.client, r.tokens, r.exchangedAt
}

// userInfoBody returns the compacted userinfo body and its full compacted
// size; the text is kept only up to diagnosticDocumentLimit+1 bytes.
func (r *diagnosticRecorder) userInfoBody() ([]byte, int) {
	if r == nil {
		return nil, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return bytes.Clone(r.userInfo.kept.Bytes()), r.userInfo.size
}

func (r *diagnosticRecorder) copyUserInfo(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.userInfo.write(p)
}

// compactCopy keeps JSON text with the whitespace outside strings removed,
// which is what json.Compact produces for valid input. size counts every
// compacted byte seen, so an oversized body still reports its compacted size.
type compactCopy struct {
	kept     bytes.Buffer
	size     int
	inString bool
	escaped  bool
}

func (c *compactCopy) write(p []byte) {
	for _, b := range p {
		if !c.inString && (b == ' ' || b == '\t' || b == '\n' || b == '\r') {
			continue
		}
		switch {
		case c.escaped:
			c.escaped = false
		case c.inString && b == '\\':
			c.escaped = true
		case b == '"':
			c.inString = !c.inString
		}
		c.size++
		if c.kept.Len() <= diagnosticDocumentLimit {
			c.kept.WriteByte(b)
		}
	}
}

type teeUserInfo struct {
	io.ReadCloser
	recorder *diagnosticRecorder
}

func (t teeUserInfo) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	if n > 0 {
		t.recorder.copyUserInfo(p[:n])
	}
	return n, err
}

// recordingTransport reports token, userinfo and JWKS requests to the
// recorder on the request context, and passes every request through
// unchanged. Only the userinfo body is copied; the token response carries
// credentials and is never read here.
type recordingTransport struct {
	base      http.RoundTripper
	endpoints map[string]upstreamEndpoint
}

func newRecordingTransport(base http.RoundTripper, resolved ResolvedConfig) *recordingTransport {
	endpoints := map[string]upstreamEndpoint{}
	for raw, endpoint := range map[string]upstreamEndpoint{resolved.TokenEndpoint: upstreamToken, resolved.UserInfoEndpoint: upstreamUserInfo, resolved.JWKSEndpoint: upstreamJWKS} {
		if u, err := url.Parse(raw); err == nil && raw != "" {
			endpoints[endpointKey(u)] = endpoint
		}
	}
	return &recordingTransport{base: base, endpoints: endpoints}
}

func endpointKey(u *url.URL) string { return u.Scheme + "://" + u.Host + u.EscapedPath() }

func (t *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	recorder := recorderFrom(r.Context())
	endpoint, ok := t.endpoints[endpointKey(r.URL)]
	if recorder == nil || !ok {
		return t.base.RoundTrip(r)
	}
	begin := time.Now()
	response, err := t.base.RoundTrip(r)
	recorder.request(endpoint, response, time.Since(begin))
	if err == nil && endpoint == upstreamUserInfo && response.Body != nil {
		response.Body = teeUserInfo{ReadCloser: response.Body, recorder: recorder}
	}
	return response, err
}
