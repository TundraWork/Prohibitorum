package oidc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

func TestExplicitOIDCClientPreservesTokenVerification(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid", "signature", "issuer", "audience", "nonce", "expired", "algorithm", "access token hash", "missing id token"} {
		t.Run(scenario, func(t *testing.T) {
			var issuer string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/keys" {
					_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "key", Use: "sig", Algorithm: "ES256"}}})
					return
				}
				claims := map[string]any{"iss": issuer, "sub": "subject", "aud": "client", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": "nonce"}
				signingKey := jose.SigningKey{Algorithm: jose.ES256, Key: key}
				switch scenario {
				case "signature":
					signingKey.Key = other
				case "issuer":
					claims["iss"] = "https://other.example"
				case "audience":
					claims["aud"] = "other-client"
				case "nonce":
					claims["nonce"] = "other-nonce"
				case "expired":
					claims["exp"] = time.Now().Add(-time.Hour).Unix()
				case "algorithm":
					signingKey = jose.SigningKey{Algorithm: jose.HS256, Key: []byte("01234567890123456789012345678901")}
				case "access token hash":
					claims["at_hash"] = "invalid-hash"
				}
				signer, err := jose.NewSigner(signingKey, (&jose.SignerOptions{}).WithHeader("kid", "key"))
				if err != nil {
					t.Error(err)
					return
				}
				raw, _ := json.Marshal(claims)
				signed, err := signer.Sign(raw)
				if err != nil {
					t.Error(err)
					return
				}
				token, err := signed.CompactSerialize()
				if err != nil {
					t.Error(err)
					return
				}
				response := map[string]any{"access_token": "access", "token_type": "Bearer", "expires_in": 3600, "id_token": token}
				if scenario == "missing id token" {
					delete(response, "id_token")
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer ts.Close()
			issuer = ts.URL
			client, err := NewClient(context.Background(), "client", "secret", "https://rp.example/cb", ResolvedConfig{Issuer: issuer, TokenEndpoint: issuer + "/token", JWKSEndpoint: issuer + "/keys", TokenAuthMethod: "client_secret_basic", PKCEMethod: "off"}, nil, true)
			if err != nil {
				t.Fatal(err)
			}
			tokens, err := client.Exchange(context.Background(), "code", "", issuer, "nonce")
			if scenario == "valid" {
				if err != nil || tokens.Subject != "subject" {
					t.Fatalf("valid token: %v", err)
				}
			} else if err == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}
}

func TestExplicitOIDCClientChecksUserInfoSubject(t *testing.T) {
	for _, subject := range []string{"expected", "different"} {
		t.Run(subject, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer access" {
					t.Error("missing access token")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"sub": subject, "picture": "https://cdn.example/avatar"})
			}))
			defer ts.Close()
			client, err := NewClient(context.Background(), "client", "secret", "https://rp.example/cb", ResolvedConfig{Issuer: ts.URL, UserInfoEndpoint: ts.URL, TokenAuthMethod: "client_secret_post", PKCEMethod: "off"}, nil, true)
			if err != nil {
				t.Fatal(err)
			}
			info, err := client.UserInfo(context.Background(), "access", "expected")
			if subject == "expected" {
				if err != nil || info["picture"] != "https://cdn.example/avatar" {
					t.Fatalf("userinfo: %v %v", info, err)
				}
			} else if err == nil {
				t.Fatal("mismatched subject accepted")
			}
		})
	}
}
