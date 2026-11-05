package authn

import (
	"context"
	"testing"

	"prohibitorum/pkg/kv"
)

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
