package authn

import (
	"context"
	"testing"

	"prohibitorum/pkg/kv"
)

func TestSAMLConsentTicketRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	tk := SAMLConsentTicket{AccountID: 7, SPID: 42, EntityID: "https://sp.example/meta", DisplayName: "Salesforce", Attributes: []string{"Email"}, ReturnTo: "https://idp.example/saml/sso?x=1"}

	nonce, err := DemandSAMLConsent(ctx, store, tk)
	if err != nil || nonce == "" {
		t.Fatalf("demand: %v nonce=%q", err, nonce)
	}
	if _, ok, _ := PeekSAMLConsent(ctx, store, nonce, 8); ok {
		t.Fatal("peek returned a ticket bound to a different account")
	}
	got, ok, err := PeekSAMLConsent(ctx, store, nonce, 7)
	if err != nil || !ok || got.SPID != 42 || got.ReturnTo != tk.ReturnTo {
		t.Fatalf("peek: %v ok=%v got=%+v", err, ok, got)
	}
	if _, ok, _ := ConsumeSAMLConsent(ctx, store, nonce, 7); !ok {
		t.Fatal("first consume should succeed")
	}
	if _, ok, _ := ConsumeSAMLConsent(ctx, store, nonce, 7); ok {
		t.Fatal("second consume should fail (single use)")
	}
}
