package authn

import (
	"context"
	"testing"

	"prohibitorum/pkg/kv"
)

func TestResumeWithChoice(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"/saml/sso/resume?ticket=abc", "/saml/sso/resume?ticket=abc&account_choice=n%2B1"},
		{"/oauth/authorize", "/oauth/authorize?account_choice=n%2B1"},
		// A signed SAML query keeps its bytes and order; only the old choice goes.
		{"/saml/sso?SAMLRequest=a%2Bb&account_choice=old&SigAlg=x&Signature=s%3D", "/saml/sso?SAMLRequest=a%2Bb&SigAlg=x&Signature=s%3D&account_choice=n%2B1"},
		{"/oauth/authorize?account_choice=old", "/oauth/authorize?account_choice=n%2B1"},
		{"/oauth/authorize?account%5Fchoice=old&&x=1", "/oauth/authorize?x=1&account_choice=n%2B1"},
	} {
		if got := ResumeWithChoice(tc.in, "n+1"); got != tc.want {
			t.Errorf("ResumeWithChoice(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := StripQueryParams("a=1&reauth=x&b=2&account_choice=y", "reauth", AccountChoiceParam); got != "a=1&b=2" {
		t.Errorf("StripQueryParams = %q", got)
	}
}

func TestLookupConsentIgnoresAccount(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	nonce, err := DemandConsent(ctx, store, ConsentTicket{AccountID: 7, ClientID: "wiki"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := LookupConsent(ctx, store, nonce); err != nil || got == nil || got.AccountID != 7 {
		t.Fatalf("LookupConsent = %+v, %v", got, err)
	}
	if got, _ := LookupConsent(ctx, store, nonce); got == nil {
		t.Fatal("LookupConsent must not consume the ticket")
	}
	if got, err := LookupConsent(ctx, store, "missing"); got != nil || err != nil {
		t.Fatalf("missing ticket = %+v, %v", got, err)
	}
	samlNonce, err := DemandSAMLConsent(ctx, store, SAMLConsentTicket{AccountID: 8, EntityID: "sp"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := LookupSAMLConsent(ctx, store, samlNonce); err != nil || got == nil || got.EntityID != "sp" {
		t.Fatalf("LookupSAMLConsent = %+v, %v", got, err)
	}
}

func TestAccountChooser_CreateBindChoose(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	nonce, err := CreateAccountChooser(ctx, store, AccountChooserTicket{
		Kind: ChooserKindOIDC, App: "wiki", ResumeURL: "/oauth/authorize?client_id=wiki", LoginHint: "lin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := ChosenAccount(ctx, store, nonce, ChooserKindOIDC, "wiki"); got != 0 {
		t.Fatalf("unchosen ticket chose %d", got)
	}
	if _, err := BindAccountChooser(ctx, store, nonce, 12); err != nil {
		t.Fatal(err)
	}
	if got := ChosenAccount(ctx, store, nonce, ChooserKindOIDC, "wiki"); got != 12 {
		t.Fatalf("chosen = %d, want 12", got)
	}
	// Reusable within its lifetime, and the choice can change.
	if _, err := BindAccountChooser(ctx, store, nonce, 40); err != nil {
		t.Fatal(err)
	}
	if got := ChosenAccount(ctx, store, nonce, ChooserKindOIDC, "wiki"); got != 40 {
		t.Fatalf("re-chosen = %d, want 40", got)
	}
	// Bound to its flow: another app or kind sees nothing.
	if got := ChosenAccount(ctx, store, nonce, ChooserKindOIDC, "other"); got != 0 {
		t.Fatalf("other app chose %d", got)
	}
	if got := ChosenAccount(ctx, store, nonce, ChooserKindSAML, "wiki"); got != 0 {
		t.Fatalf("other kind chose %d", got)
	}
	tk, err := GetAccountChooser(ctx, store, nonce)
	if err != nil || tk == nil || tk.LoginHint != "lin" || tk.ResumeURL == "" {
		t.Fatalf("ticket = %+v, %v", tk, err)
	}
	if got, _ := GetAccountChooser(ctx, store, "missing"); got != nil {
		t.Fatal("missing nonce must read as nil")
	}
	if got, _ := BindAccountChooser(ctx, store, "missing", 1); got != nil {
		t.Fatal("binding a missing nonce must return nil")
	}
}
