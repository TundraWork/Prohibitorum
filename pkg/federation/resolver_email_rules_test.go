package federation_test

import (
	"context"
	"testing"

	"prohibitorum/pkg/db"
	federationoidc "prohibitorum/pkg/federation"
)

// The three PHB-148 resolver rules the review's B6 asks for, as
// mutation-proof tests: each must fail if the rule is removed.

// ruleExistingPrimarySurvives: a login that absorbs an upstream address must
// never move an existing primary. Mutant: ensurePrimaryWhenEmpty always sets.
func TestResolve_AbsorbNeverDisplacesExistingPrimary(t *testing.T) {
	q := newFakeModesQueries()
	q.identityErr = nil
	q.identityResult = db.AccountIdentity{
		ID: 301, AccountID: 51, UpstreamIdpID: 42,
		UpstreamIss: "https://issuer.example/", UpstreamSub: "sub-1",
	}
	// The account already holds a verified primary; the incoming upstream
	// address is different.
	q.accountByIDResults[51] = db.Account{ID: 51, Username: "carol", DisplayName: "Carol"}
	q.seedVerifiedPrimary(51, "current@example.test")

	tok := goodTokens() // alice@example.com, verified
	if _, err := federationoidc.Resolve(context.Background(), q, &recordingAudit{}, newIDP(federationoidc.ModeAutoProvision), tok, nil); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := q.emailPrimary[51]; got == 0 {
		t.Fatal("no primary after login")
	} else {
		row := q.emailRows[got]
		if row.Address != "current@example.test" {
			t.Fatalf("primary moved to %q; the existing primary must survive upstream absorption", row.Address)
		}
	}
}

// ruleNoEmailDoesNotClear: a login whose upstream asserts no email (Steam,
// VRChat) must leave the primary untouched. Mutant: syncClaims clears the
// primary when the upstream email is absent — the pre-PHB-148 behavior.
func TestResolve_LoginWithoutUpstreamEmailKeepsPrimary(t *testing.T) {
	q := newFakeModesQueries()
	q.identityErr = nil
	q.identityResult = db.AccountIdentity{
		ID: 302, AccountID: 52, UpstreamIdpID: 42,
		UpstreamIss: "https://issuer.example/", UpstreamSub: "sub-1",
	}
	q.accountByIDResults[52] = db.Account{ID: 52, Username: "dave", DisplayName: "Dave"}
	q.seedVerifiedPrimary(52, "keeper@example.test")

	tok := goodTokens()
	tok.Email = nil // Steam/VRChat-style: no email claim at all

	if _, err := federationoidc.Resolve(context.Background(), q, &recordingAudit{}, newIDP(federationoidc.ModeAutoProvision), tok, nil); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	primaryID, ok := q.emailPrimary[52]
	if !ok || primaryID == 0 {
		t.Fatal("primary cleared by an email-less login; it must survive")
	}
	if row := q.emailRows[primaryID]; row.Address != "keeper@example.test" {
		t.Fatalf("primary changed to %q on an email-less login", row.Address)
	}
	// And the stored address list is unchanged: nothing was deleted.
	if len(q.emailRows) != 1 {
		t.Fatalf("address rows = %d after email-less login, want 1 (no removal)", len(q.emailRows))
	}
}

// ruleUnverifiedStaysOut: an upstream address the provider did NOT assert
// verified must not enter the account's address list. Mutant: absorb
// unconditionally.
func TestResolve_UnverifiedUpstreamEmailNotAbsorbed(t *testing.T) {
	q := newFakeModesQueries()
	q.identityErr = nil
	q.identityResult = db.AccountIdentity{
		ID: 303, AccountID: 53, UpstreamIdpID: 42,
		UpstreamIss: "https://issuer.example/", UpstreamSub: "sub-1",
	}
	q.accountByIDResults[53] = db.Account{ID: 53, Username: "bob", DisplayName: "Bob"}

	tok := goodTokens()
	tok.EmailVerified = false // present but unverified

	if _, err := federationoidc.Resolve(context.Background(), q, &recordingAudit{}, newIDP(federationoidc.ModeAutoProvision), tok, nil); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, row := range q.emailRows {
		if row.AccountID == 53 {
			t.Fatalf("unverified upstream address %q entered the address list; it must stay out (only upstream_email records it)", row.Address)
		}
	}
	// The identity row still snapshots the unverified email, as before.
	if len(q.identityDataCalls) != 1 || q.identityDataCalls[0].UpstreamEmail.String != "alice@example.com" {
		t.Fatalf("identity snapshot drift: %+v", q.identityDataCalls)
	}
}

// ruleLinkAbsorbs: an explicit link (IntentLink) with a verified upstream
// address must absorb it into the linked account's address list. Mutant: the
// resolveLink absorb call removed.
func TestResolve_LinkAbsorbsVerifiedUpstreamEmail(t *testing.T) {
	q := newFakeModesQueries()
	q.accountByIDResults[60] = db.Account{ID: 60, Username: "linker", DisplayName: "Linker"}
	// An existing unverified copy of the upstream address flips to verified.
	seeded, err := q.InsertAccountEmail(context.Background(), db.InsertAccountEmailParams{AccountID: 60, Address: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	added := seeded.ID

	accountID := int32(60)
	resolver := federationoidc.NewResolver(q, &recordingAudit{}, nil)
	if _, err := resolver.ResolveIdentity(context.Background(), genericProvider(federationoidc.ModeAutoProvision), *goodTokens(), federationoidc.ResolveContext{
		Intent: federationoidc.IntentLink, LinkAccountID: &accountID,
	}); err != nil {
		t.Fatalf("ResolveIdentity: %v", err)
	}
	row, ok := q.emailRows[added]
	if !ok || !row.VerifiedAt.Valid {
		t.Fatal("link did not absorb the verified upstream address; the existing copy must flip to verified")
	}
	if row.VerifiedVia.String != "upstream" {
		t.Fatalf("absorbed via %q, want upstream", row.VerifiedVia.String)
	}
}
