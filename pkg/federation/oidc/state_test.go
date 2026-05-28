package oidc_test

import (
	"testing"

	federationoidc "prohibitorum/pkg/federation/oidc"
)

func TestState_EncodeDecodeRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   federationoidc.FedState
	}{
		{
			name: "login flow (no linking account)",
			in: federationoidc.FedState{
				IDPID:                 42,
				IDPSlug:               "google",
				ExpectedIss:           "https://accounts.google.com",
				ExpectedTokenEndpoint: "https://oauth2.googleapis.com/token",
				Nonce:                 "n-12345",
				CodeVerifier:          "v-abcdef0123456789",
				ReturnTo:              "/dashboard",
				LinkingAccountID:      nil,
			},
		},
		{
			name: "link flow (linking account set)",
			in: federationoidc.FedState{
				IDPID:                 7,
				IDPSlug:               "okta",
				ExpectedIss:           "https://example.okta.com",
				ExpectedTokenEndpoint: "https://example.okta.com/oauth2/v1/token",
				Nonce:                 "n-link",
				CodeVerifier:          "v-link",
				ReturnTo:              "/me/identities",
				LinkingAccountID:      int32Ptr(99),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc, err := tc.in.Encode()
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			got, err := federationoidc.DecodeFedState(enc)
			if err != nil {
				t.Fatalf("DecodeFedState: %v", err)
			}
			if got.IDPID != tc.in.IDPID ||
				got.IDPSlug != tc.in.IDPSlug ||
				got.ExpectedIss != tc.in.ExpectedIss ||
				got.ExpectedTokenEndpoint != tc.in.ExpectedTokenEndpoint ||
				got.Nonce != tc.in.Nonce ||
				got.CodeVerifier != tc.in.CodeVerifier ||
				got.ReturnTo != tc.in.ReturnTo {
				t.Fatalf("scalar fields: got %+v, want %+v", got, tc.in)
			}
			switch {
			case tc.in.LinkingAccountID == nil && got.LinkingAccountID != nil:
				t.Fatalf("LinkingAccountID: got %v, want nil", *got.LinkingAccountID)
			case tc.in.LinkingAccountID != nil && got.LinkingAccountID == nil:
				t.Fatalf("LinkingAccountID: got nil, want %v", *tc.in.LinkingAccountID)
			case tc.in.LinkingAccountID != nil && got.LinkingAccountID != nil:
				if *got.LinkingAccountID != *tc.in.LinkingAccountID {
					t.Fatalf("LinkingAccountID: got %d, want %d", *got.LinkingAccountID, *tc.in.LinkingAccountID)
				}
			}
		})
	}
}

func TestState_KeysSeparated(t *testing.T) {
	const token = "the-same-token"
	login := federationoidc.LoginKey(token)
	link := federationoidc.LinkKey(token)
	if login == link {
		t.Fatalf("LoginKey and LinkKey collide for token %q: both = %q", token, login)
	}
	if login == "" || link == "" {
		t.Fatalf("empty key: login=%q link=%q", login, link)
	}
}

func TestState_DecodeRejectsGarbage(t *testing.T) {
	if _, err := federationoidc.DecodeFedState("{not json"); err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

func int32Ptr(v int32) *int32 { return &v }
