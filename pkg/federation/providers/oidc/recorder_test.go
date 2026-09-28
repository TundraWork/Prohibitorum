package oidc

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type cannedTransport struct {
	bodies map[string]string
	calls  int
}

func (t *cannedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.calls++
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(t.bodies[r.URL.Path])), Request: r}, nil
}

func recordingFixture() (*recordingTransport, *cannedTransport) {
	base := &cannedTransport{bodies: map[string]string{
		"/token":    `{"access_token": "never-copied", "token_type": "Bearer"}`,
		"/userinfo": "{\n  \"sub\": \"a b\",\n  \"note\": \"say \\\"hi\\\" {, }\"\n}\n",
	}}
	return newRecordingTransport(base, ResolvedConfig{TokenEndpoint: "https://idp.example/token", UserInfoEndpoint: "https://idp.example/userinfo", JWKSEndpoint: "https://idp.example/jwks"}), base
}

func roundTrip(t *testing.T, ctx context.Context, transport http.RoundTripper, path string) string {
	t.Helper()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://idp.example"+path, nil)
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return string(body)
}

func TestRecorderTransportWithoutRecorderOnlyForwards(t *testing.T) {
	transport, base := recordingFixture()
	if got := roundTrip(t, context.Background(), transport, "/userinfo"); got != base.bodies["/userinfo"] {
		t.Fatalf("body=%q", got)
	}
	if base.calls != 1 {
		t.Fatalf("calls=%d", base.calls)
	}
	var recorder *diagnosticRecorder
	recorder.exchanged(nil, nil)
	if body, size := recorder.userInfoBody(); body != nil || size != 0 {
		t.Fatal("nil recorder recorded")
	}
}

func TestRecorderCopiesUserInfoButNotToken(t *testing.T) {
	transport, base := recordingFixture()
	ctx, recorder := withRecorder(context.Background())
	if got := roundTrip(t, ctx, transport, "/token"); got != base.bodies["/token"] {
		t.Fatalf("token body=%q", got)
	}
	if body, size := recorder.userInfoBody(); len(body) != 0 || size != 0 {
		t.Fatalf("token body copied: %q", body)
	}
	if got := roundTrip(t, ctx, transport, "/userinfo"); got != base.bodies["/userinfo"] {
		t.Fatalf("caller saw %q", got)
	}
	body, size := recorder.userInfoBody()
	if want := `{"sub":"a b","note":"say \"hi\" {, }"}`; string(body) != want || size != len(want) {
		t.Fatalf("copy=%q size=%d", body, size)
	}
	if call := recorder.call(upstreamUserInfo); call.count != 1 || call.status != 200 {
		t.Fatalf("userinfo call=%+v", call)
	}
	if call := recorder.call(upstreamToken); call.count != 1 || call.status != 200 {
		t.Fatalf("token call=%+v", call)
	}
	roundTrip(t, ctx, transport, "/other")
	if call := recorder.call(upstreamJWKS); call.count != 0 {
		t.Fatal("unmatched endpoint recorded")
	}
}

func TestRecorderMarksOversizedUserInfo(t *testing.T) {
	transport, base := recordingFixture()
	base.bodies["/userinfo"] = `{"pad": "` + strings.Repeat("x", diagnosticDocumentLimit) + `"}`
	ctx, recorder := withRecorder(context.Background())
	if got := roundTrip(t, ctx, transport, "/userinfo"); got != base.bodies["/userinfo"] {
		t.Fatal("caller body changed")
	}
	body, size := recorder.userInfoBody()
	if len(body) != diagnosticDocumentLimit+1 || size != len(base.bodies["/userinfo"])-1 {
		t.Fatalf("kept=%d size=%d", len(body), size)
	}
	if document := diagnosticDocument(body, size); document.JSON != "" || document.OmittedBytes != size {
		t.Fatalf("document=%+v", document)
	}
}
