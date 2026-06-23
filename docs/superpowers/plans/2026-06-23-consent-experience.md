# Consent Experience Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mature the OIDC consent screen with honest "remembered next time" copy and an incremental-consent highlight, and add an advisory SAML acknowledgement so every protocol exposes a real, user-managed "connected" signal, surfaced uniformly through `/me/consent`.

**Architecture:** SAML gains a `saml_consent` table (row present = acknowledged), interposed in both `HandleSSO` and `HandleIdPInitiated` via the existing bounce-and-return pattern (signed query preserved; `ReturnTo` held server-side in a single-use KV ticket mirroring OIDC consent). A new `/saml-consent` SPA screen shares a `ConsentCard` with the OIDC screen. `/me/consent` merges OIDC consents and SAML acks behind a `kind` discriminator; revoke routes by kind.

**Tech Stack:** Go (pgx, sqlc, huma HTTP handlers, crewjam/saml), PostgreSQL, Vue 3 + Vite + reka-ui + vue-i18n, vitest, `cmd/smoke`.

**Spec:** `docs/superpowers/specs/2026-06-23-consent-experience-design.md`

---

## File structure

| File | Responsibility |
|------|----------------|
| `db/migrations/022_saml_consent.sql` | New `saml_consent` table (create) |
| `db/queries/saml_consent.sql` | sqlc queries: Has/Upsert/Delete/List (create) |
| `pkg/db/saml_consent.sql.go`, `querier.go`, `models.go` | sqlc-generated (regenerate) |
| `pkg/authn/saml_consent.go` (+ `_test.go`) | Single-use KV consent ticket helpers |
| `pkg/protocol/saml/consent_saml.go` (+ `_test.go`) | `attributeLabels` + `maybeDemandSAMLConsent` gate |
| `pkg/protocol/saml/sso.go`, `sso_init.go` | Interpose the gate (modify) |
| `pkg/contract/saml_consent.go` | SAML consent context/decision DTOs (create) |
| `pkg/server/handle_saml_consent.go` (+ `_test.go`) | GET context + POST decision endpoints |
| `pkg/server/server.go:388-390` | Register the two new routes (modify) |
| `pkg/contract/auth.go:363-367` | `ConsentContext.AlreadyGranted` (modify) |
| `pkg/server/handle_consent.go:33-48` | OIDC context returns already-granted (modify) |
| `pkg/contract/launchpad.go` | `ConsentedApp.Kind`, `RevokeConsentInput.Kind` (modify) |
| `pkg/server/handle_me_consent.go` | Merge OIDC + SAML; revoke by kind (modify) |
| `dashboard/src/components/custom/ConsentCard.vue` | Shared consent layout (create) |
| `dashboard/src/pages/ConsentView.vue` | Use ConsentCard + maturity copy (modify) |
| `dashboard/src/pages/SamlConsentView.vue` | SAML advisory screen (create) |
| `dashboard/src/router/index.ts` | `/saml-consent` route (modify) |
| `dashboard/src/pages/AppAccessView.vue` | List/revoke both kinds (modify) |
| `dashboard/src/locales/en.ts`, `zh.ts` | New consent strings (modify) |
| `cmd/smoke/*` | SAML advisory-consent arc (modify) |
| `pkg/webui/dist/*` | Rebuilt SPA bundle (regenerate) |

---

### Task 1: `saml_consent` table + queries (DB layer)

**Goal:** Persist a per-(account, SP) advisory acknowledgement and expose sqlc accessors.

**Files:**
- Create: `db/migrations/022_saml_consent.sql`
- Create: `db/queries/saml_consent.sql`
- Modify: `sqlc.yaml` (add `int32`/`int64` overrides for the two new columns)
- Modify (generated): `pkg/db/saml_consent.sql.go`, `pkg/db/querier.go`, `pkg/db/models.go`
- Test: `pkg/db` builds; query exercised by later tasks (no standalone DB unit test here — the repo tests queries through handlers/smoke).

**Acceptance Criteria:**
- [ ] sqlc produces `HasSAMLConsent`, `UpsertSAMLConsent`, `DeleteSAMLConsent`, `ListSAMLConsentsByAccount` on `db.Querier`.
- [ ] The generated params use `AccountID int32` / `SpID int64` (not `pgtype.Int4/Int8`) — i.e. the `sqlc.yaml` overrides are present.
- [ ] `go build -tags nodynamic ./...` passes.
- [ ] The migration is picked up by `db/migrations/migrations.go` (`//go:embed *.sql`).

**Verify:** `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate && go build -tags nodynamic ./...` → no errors; `git grep -l HasSAMLConsent pkg/db` shows the generated file.

> Tooling note: `sqlc` is NOT installed (the goenv shim is empty). Regenerate with the pinned version via `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate` (verified working). The schema header pins `sqlc v1.30.0`.

**Steps:**

- [ ] **Step 1: Write the migration**

Create `db/migrations/022_saml_consent.sql`:

```sql
-- 022_saml_consent.sql — per-(account, SP) advisory acknowledgement that the
-- user agreed to sign in to a SAML service provider. Advisory only: a row's
-- presence means "acknowledged". Mirrors oidc_consent. Revoked by deleting the
-- row; re-acknowledged on the next SSO. CASCADEs with the account and the SP.
CREATE TABLE saml_consent (
  account_id integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  sp_id      bigint  NOT NULL REFERENCES saml_sp(id)  ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (account_id, sp_id)
);
```

- [ ] **Step 2: Confirm migration auto-discovery**

Run: `sed -n '1,40p' db/migrations/migrations.go`
Expected: an `//go:embed *.sql` (or `embed.FS` over the dir). If it is a manual file list instead, append `"022_saml_consent.sql"` in the same style. Do not change the loader otherwise.

- [ ] **Step 3: Write the queries**

Create `db/queries/saml_consent.sql`:

```sql
-- name: HasSAMLConsent :one
SELECT EXISTS (
  SELECT 1 FROM saml_consent WHERE account_id = $1 AND sp_id = $2
) AS has;

-- name: UpsertSAMLConsent :exec
INSERT INTO saml_consent (account_id, sp_id, created_at, updated_at)
VALUES ($1, $2, now(), now())
ON CONFLICT (account_id, sp_id)
DO UPDATE SET updated_at = now();

-- name: DeleteSAMLConsent :exec
DELETE FROM saml_consent WHERE account_id = $1 AND sp_id = $2;

-- name: ListSAMLConsentsByAccount :many
SELECT sc.sp_id, sp.entity_id, sp.display_name, sc.updated_at
FROM saml_consent sc
JOIN saml_sp sp ON sp.id = sc.sp_id
WHERE sc.account_id = $1
ORDER BY sp.display_name;
```

- [ ] **Step 4: Add sqlc type overrides**

The repo maps every integer/bigint PK/FK explicitly because sqlc's pgx/v5 default for these is `pgtype.Int4`/`pgtype.Int8`, not `int32`/`int64`. Add to the `overrides:` list in `sqlc.yaml` (alongside the existing `oidc_consent.account_id` / `saml_sp.id` entries):

```yaml
          - column: "saml_consent.account_id"
            go_type: "int32"
          - column: "saml_consent.sp_id"
            go_type: "int64"
```

- [ ] **Step 5: Regenerate sqlc and build**

`sqlc` is not installed locally; run the pinned version via `go run`.

Run: `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate && go build -tags nodynamic ./...`
Expected: PASS. `HasSAMLConsentParams{AccountID int32; SpID int64}`, `UpsertSAMLConsentParams`, `DeleteSAMLConsentParams`, and `ListSAMLConsentsByAccountRow{SpID int64; EntityID string; DisplayName string; UpdatedAt pgtype.Timestamptz}` now exist. (If `AccountID`/`SpID` came out as `pgtype.Int4/Int8`, the Step-4 overrides are missing — add them and regenerate.)

- [ ] **Step 6: Commit**

```bash
git add db/migrations/022_saml_consent.sql db/queries/saml_consent.sql sqlc.yaml pkg/db/
git commit -m "feat(saml): saml_consent table + sqlc queries"
```

---

### Task 2: SAML consent KV ticket helpers (authn)

**Goal:** A single-use, account-bound, 10-minute KV ticket carrying the SAML consent context — mirrors `pkg/authn/consent.go`.

**Files:**
- Create: `pkg/authn/saml_consent.go`
- Test: `pkg/authn/saml_consent_test.go`

**Acceptance Criteria:**
- [ ] `DemandSAMLConsent` mints a nonce and stores the ticket; `PeekSAMLConsent`/`ConsumeSAMLConsent` return it only for the matching account; consume is single-use.

**Verify:** `go test ./pkg/authn/ -run SAMLConsent -v` → PASS.

**Steps:**

- [ ] **Step 1: Write the failing test**

Create `pkg/authn/saml_consent_test.go`:

```go
package authn

import (
	"context"
	"testing"

	"prohibitorum/pkg/kv"
)

func TestSAMLConsentTicketRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemory()
	tk := SAMLConsentTicket{AccountID: 7, SPID: 42, EntityID: "https://sp.example/meta", DisplayName: "Salesforce", Attributes: []string{"Email"}, ReturnTo: "https://idp.example/saml/sso?x=1"}

	nonce, err := DemandSAMLConsent(ctx, store, tk)
	if err != nil || nonce == "" {
		t.Fatalf("demand: %v nonce=%q", err, nonce)
	}

	// Wrong account → not found.
	if _, ok, _ := PeekSAMLConsent(ctx, store, nonce, 8); ok {
		t.Fatal("peek returned a ticket bound to a different account")
	}
	// Right account → found, not consumed.
	got, ok, err := PeekSAMLConsent(ctx, store, nonce, 7)
	if err != nil || !ok || got.SPID != 42 || got.ReturnTo != tk.ReturnTo {
		t.Fatalf("peek: %v ok=%v got=%+v", err, ok, got)
	}
	// Consume pops it (single use).
	if _, ok, _ := ConsumeSAMLConsent(ctx, store, nonce, 7); !ok {
		t.Fatal("first consume should succeed")
	}
	if _, ok, _ := ConsumeSAMLConsent(ctx, store, nonce, 7); ok {
		t.Fatal("second consume should fail (single use)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/authn/ -run SAMLConsent -v`
Expected: FAIL — `undefined: SAMLConsentTicket` / `DemandSAMLConsent`. (If `kv.NewMemory` differs, check `pkg/kv` for the in-memory constructor name and adjust the test.)

- [ ] **Step 3: Write the implementation**

Create `pkg/authn/saml_consent.go`:

```go
package authn

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"

	"prohibitorum/pkg/kv"
)

const samlConsentKeyPrefix = "saml:consent:"

// SAMLConsentTicket is the server-minted record of a pending SAML advisory
// acknowledgement. Stored in KV under a single-use nonce; the browser only ever
// carries the opaque nonce. ReturnTo is the exact inbound SSO URL (signed raw
// query preserved) so the assertion flow resumes verbatim after the ack.
type SAMLConsentTicket struct {
	AccountID   int32    `json:"account_id"`
	SPID        int64    `json:"sp_id"`
	EntityID    string   `json:"entity_id"`
	DisplayName string   `json:"display_name"`
	Attributes  []string `json:"attributes"`
	ReturnTo    string   `json:"return_to"`
}

// DemandSAMLConsent mints a single-use nonce and stores the ticket (reuses the
// OIDC ConsentTicketTTL).
func DemandSAMLConsent(ctx context.Context, store kv.Store, ticket SAMLConsentTicket) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(buf)
	payload, err := json.Marshal(ticket)
	if err != nil {
		return "", err
	}
	if err := store.SetEx(ctx, samlConsentKeyPrefix+nonce, string(payload), ConsentTicketTTL); err != nil {
		return "", err
	}
	return nonce, nil
}

// PeekSAMLConsent reads (without consuming) and returns the ticket iff it
// belongs to accountID.
func PeekSAMLConsent(ctx context.Context, store kv.Store, nonce string, accountID int32) (*SAMLConsentTicket, bool, error) {
	if nonce == "" {
		return nil, false, nil
	}
	val, err := store.Get(ctx, samlConsentKeyPrefix+nonce)
	if errors.Is(err, kv.ErrKeyNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return decodeSAMLConsent(val, accountID)
}

// ConsumeSAMLConsent atomically pops the ticket (single use) iff it belongs to
// accountID.
func ConsumeSAMLConsent(ctx context.Context, store kv.Store, nonce string, accountID int32) (*SAMLConsentTicket, bool, error) {
	if nonce == "" {
		return nil, false, nil
	}
	val, err := store.Pop(ctx, samlConsentKeyPrefix+nonce)
	if errors.Is(err, kv.ErrKeyNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return decodeSAMLConsent(val, accountID)
}

func decodeSAMLConsent(val string, accountID int32) (*SAMLConsentTicket, bool, error) {
	var t SAMLConsentTicket
	if err := json.Unmarshal([]byte(val), &t); err != nil {
		return nil, false, nil // malformed → treat as absent
	}
	if t.AccountID != accountID {
		return nil, false, nil // bound to a different account
	}
	return &t, true, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/authn/ -run SAMLConsent -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/authn/saml_consent.go pkg/authn/saml_consent_test.go
git commit -m "feat(saml): single-use KV consent ticket helpers"
```

---

### Task 3: SAML consent gate + interposition

**Goal:** Derive the friendly attribute labels and, when the account has no ack for the SP, bounce the browser to `/saml-consent` (preserving the signed SSO URL) instead of issuing — in both SAML flows.

**Files:**
- Create: `pkg/protocol/saml/consent_saml.go`
- Test: `pkg/protocol/saml/consent_saml_test.go`
- Modify: `pkg/protocol/saml/sso.go` (after the `req.ForceAuthn` block, before `consumeAuthnRequestID`)
- Modify: `pkg/protocol/saml/sso_init.go` (after the RBAC authz block, before the rate-limit)

**Acceptance Criteria:**
- [ ] `attributeLabels` returns de-duplicated friendly names (FriendlyName, else Name) from an attribute_map JSON array; `nil`/empty/malformed → empty slice.
- [ ] With no ack row, both handlers `302` to `/saml-consent?ticket=…` and issue no assertion.
- [ ] With an ack row, both handlers proceed unchanged.
- [ ] Passive SP-initiated requests (`IsPassive`) skip the gate (silent SSO is not blocked on an advisory screen).

**Verify:** `go test ./pkg/protocol/saml/ -run 'AttributeLabels|Consent' -v` → PASS.

**Steps:**

- [ ] **Step 1: Write the failing test**

Create `pkg/protocol/saml/consent_saml_test.go`:

```go
package saml

import "testing"

func TestAttributeLabels(t *testing.T) {
	// Ordered JSONB array shape from saml_sp.attribute_map (attrMapEntry).
	mapJSON := []byte(`[
		{"name":"urn:oid:0.9.2342.19200300.100.1.3","friendly_name":"Email","source":"email"},
		{"name":"http://schemas.../name","friendly_name":"","source":"display_name"},
		{"name":"urn:oid:0.9.2342.19200300.100.1.3","friendly_name":"Email","source":"email"}
	]`)
	got := attributeLabels(mapJSON)
	want := []string{"Email", "http://schemas.../name"}
	if len(got) != len(want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("labels = %v, want %v", got, want)
		}
	}
	if l := attributeLabels([]byte("not json")); len(l) != 0 {
		t.Fatalf("malformed map should yield no labels, got %v", l)
	}
	if l := attributeLabels([]byte("[]")); len(l) != 0 {
		t.Fatalf("empty map should yield no labels, got %v", l)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/protocol/saml/ -run AttributeLabels -v`
Expected: FAIL — `undefined: attributeLabels`. (Confirm `attrMapEntry`'s JSON tags in `pkg/protocol/saml/attributes.go` — `name`, `friendly_name` — match the test fixture; adjust the fixture if a tag differs.)

- [ ] **Step 3: Write the gate + label helper**

Create `pkg/protocol/saml/consent_saml.go`:

```go
package saml

import (
	"encoding/json"
	"net/http"
	"net/url"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// attributeLabels turns an SP's attribute_map (the ordered JSONB array of
// attrMapEntry) into a de-duplicated, order-preserving list of human labels for
// the advisory consent screen — FriendlyName when set, else the raw Name.
// Malformed/empty input yields no labels (the screen shows a generic fallback).
func attributeLabels(mapJSON []byte) []string {
	var entries []attrMapEntry
	if err := json.Unmarshal(mapJSON, &entries); err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(entries))
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		label := e.FriendlyName
		if label == "" {
			label = e.Name
		}
		if label == "" {
			continue
		}
		if _, dup := seen[label]; dup {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	return out
}

// maybeDemandSAMLConsent gates assertion issuance on a stored advisory ack.
// Returns redirected=true when it has written a 302 to /saml-consent (the caller
// MUST return); false to proceed with issuance. The returnTo is the exact inbound
// SSO URL so the assertion flow resumes verbatim after the ack (the signed raw
// query is preserved, exactly like the forced-re-auth bounce).
func (i *IdP) maybeDemandSAMLConsent(w http.ResponseWriter, r *http.Request, accountID int32, sp db.SamlSp) (redirected bool, err error) {
	has, herr := i.queries.HasSAMLConsent(r.Context(), db.HasSAMLConsentParams{
		AccountID: accountID,
		SpID:      sp.ID,
	})
	if herr != nil {
		return false, herr
	}
	if has {
		return false, nil
	}
	nonce, derr := authn.DemandSAMLConsent(r.Context(), i.kv, authn.SAMLConsentTicket{
		AccountID:   accountID,
		SPID:        sp.ID,
		EntityID:    sp.EntityID,
		DisplayName: sp.DisplayName,
		Attributes:  attributeLabels(sp.AttributeMap),
		ReturnTo:    i.baseURL() + r.URL.RequestURI(),
	})
	if derr != nil {
		return false, derr
	}
	http.Redirect(w, r, i.baseURL()+"/saml-consent?ticket="+url.QueryEscape(nonce), http.StatusFound)
	return true, nil
}
```

> Note: `sp` is the SP row both flows already hold (`req.SP` in `HandleSSO`, the `GetSAMLSPByEntityID` result in `HandleIdPInitiated`). Confirm both are `db.SamlSp`; if `req.SP` is a wrapper, pass `req.SP` field that holds the `db.SamlSp` (it exposes `.ID/.EntityID/.DisplayName/.AttributeMap`, used today at `sso.go:343`).

- [ ] **Step 4: Interpose in `HandleSSO`**

In `pkg/protocol/saml/sso.go`, immediately AFTER the `if req.ForceAuthn { … }` block (ends ~line 291) and BEFORE `// (4-replay) … consumeAuthnRequestID` (~line 295), insert:

```go
	// Advisory consent gate — interactive only. A passive request cannot render
	// the screen, so silent SSO proceeds without collecting the (advisory) ack.
	// Placed after the re-auth gate and BEFORE the single-use replay consume, so
	// the consent bounce can return and re-run without tripping replay (the same
	// reason the replay consume sits below the re-auth bounce).
	if !req.IsPassive {
		if redirected, cerr := i.maybeDemandSAMLConsent(w, r, sess.Data.AccountID, sp); cerr != nil {
			i.errorPage(w, r, "server_error")
			return
		} else if redirected {
			return
		}
	}
```

- [ ] **Step 5: Interpose in `HandleIdPInitiated`**

In `pkg/protocol/saml/sso_init.go`, immediately AFTER the RBAC authz block (the `if !authzed.Bool { … }` ending ~line 136) and BEFORE the `// Per-account + per-SP rate limit` block (~line 138), insert:

```go
	// Advisory consent gate. IdP-initiated SSO is always interactive (no
	// IsPassive), so always honor the gate. Placed after RBAC and before the
	// rate limit / build / persist so nothing is issued for an un-acknowledged SP.
	if redirected, cerr := i.maybeDemandSAMLConsent(w, r, account.ID, sp); cerr != nil {
		i.errorPage(w, r, "server_error")
		return
	} else if redirected {
		return
	}
```

- [ ] **Step 6: Add handler tests**

Append to `pkg/protocol/saml/consent_saml_test.go` two tests that drive the existing SAML SSO test harness (mirror the closest existing test in `pkg/protocol/saml/sso_test.go` / `sso_init_test.go` for fixture setup — same SP, account, session, and signing-key wiring):
- `TestHandleIdPInitiated_NoConsent_RedirectsToScreen`: no `saml_consent` row → response is `302` with `Location` starting `…/saml-consent?ticket=`, and no auto-POST form body.
- `TestHandleIdPInitiated_WithConsent_Issues`: insert the ack (`UpsertSAMLConsent`) → response is the auto-POST form (contains `SAMLResponse`).

Use the same in-test `db.Querier` the existing SAML tests use (real `*db.Queries` against the test DB, or the package's stub). Match the existing helper names rather than inventing new ones.

- [ ] **Step 7: Run tests**

Run: `go test ./pkg/protocol/saml/ -run 'AttributeLabels|Consent' -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add pkg/protocol/saml/consent_saml.go pkg/protocol/saml/consent_saml_test.go pkg/protocol/saml/sso.go pkg/protocol/saml/sso_init.go
git commit -m "feat(saml): interpose advisory consent gate in both SSO flows"
```

---

### Task 4: SAML consent endpoints (context + decision)

**Goal:** Serve the advisory screen's data and record the decision, redirecting back to the in-flight SSO URL on approve.

**Files:**
- Create: `pkg/contract/saml_consent.go`
- Create: `pkg/server/handle_saml_consent.go`
- Test: `pkg/server/handle_saml_consent_test.go`
- Modify: `pkg/server/server.go` (register two routes after line 390)

**Acceptance Criteria:**
- [ ] `GET /api/prohibitorum/saml-consent?ticket=` returns `{ sp{id,displayName,logoUri}, account{displayName}, attributes[] }` for a valid ticket bound to the session; invalid → error.
- [ ] `POST` `approve` writes the ack and returns `{ redirect: ticket.ReturnTo }`; `decline` returns `{ redirect: "/" }`; the ticket is single-use.

**Verify:** `go test ./pkg/server/ -run SAMLConsent -v` → PASS.

**Steps:**

- [ ] **Step 1: Write the contract DTOs**

Create `pkg/contract/saml_consent.go`:

```go
package contract

// SAMLConsentContext is GET /api/prohibitorum/saml-consent — what the advisory
// SAML screen renders. Advisory only: Attributes is informational (no toggles).
type SAMLConsentContext struct {
	SP         SAMLConsentSP `json:"sp"`
	Account    ConsentUser   `json:"account"`
	Attributes []string      `json:"attributes"`
}

type SAMLConsentSP struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	LogoURI     string `json:"logoUri,omitempty"`
}

// SAMLConsentDecision is the POST body. Decision is "approve" or "decline".
type SAMLConsentDecision struct {
	Ticket   string `json:"ticket"`
	Decision string `json:"decision"`
}
```

(`ConsentUser` and `ConsentResult` are reused from `pkg/contract/auth.go`.)

- [ ] **Step 2: Write the handlers**

Create `pkg/server/handle_saml_consent.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// GET /api/prohibitorum/saml-consent?ticket=
func (s *Server) handleSAMLConsentContextHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Data == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	ticket, ok, err := authn.PeekSAMLConsent(r.Context(), s.kvStore, r.URL.Query().Get("ticket"), sess.Data.AccountID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if !ok {
		writeAuthErr(w, authn.ErrInvalidConsentTicket())
		return
	}
	id := strconv.FormatInt(ticket.SPID, 10)
	var logo string
	if etag, e := s.queries.GetEntityIconEtag(r.Context(), db.GetEntityIconEtagParams{OwnerKind: "saml_sp", OwnerID: id}); e == nil {
		if u := entityIconURLPtr("saml_sp", id, etag); u != nil {
			logo = *u
		}
	}
	out := contract.SAMLConsentContext{
		SP:         contract.SAMLConsentSP{ID: id, DisplayName: ticket.DisplayName, LogoURI: logo},
		Account:    contract.ConsentUser{DisplayName: sess.Account.DisplayName},
		Attributes: ticket.Attributes,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// POST /api/prohibitorum/saml-consent
func (s *Server) handleSAMLConsentDecisionHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Data == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	var in contract.SAMLConsentDecision
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	ticket, ok, err := authn.ConsumeSAMLConsent(r.Context(), s.kvStore, in.Ticket, sess.Data.AccountID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if !ok {
		writeAuthErr(w, authn.ErrInvalidConsentTicket())
		return
	}

	var redirect string
	switch in.Decision {
	case "approve":
		if uerr := s.queries.UpsertSAMLConsent(r.Context(), db.UpsertSAMLConsentParams{
			AccountID: sess.Data.AccountID, SpID: ticket.SPID,
		}); uerr != nil {
			writeAuthErr(w, uerr)
			return
		}
		// ReturnTo is server-minted (our own origin, exact SSO URL) — trusted.
		redirect = ticket.ReturnTo
	case "decline":
		// The user stays signed in to the IdP; they just don't enter the app.
		redirect = "/"
	default:
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(contract.ConsentResult{Redirect: redirect})
}
```

- [ ] **Step 3: Register the routes**

In `pkg/server/server.go`, after the OIDC consent registration (line 390), add:

```go
	// SAML advisory consent (UI context + decision), mirroring the OIDC pair.
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/saml-consent", sessionReq, s.handleSAMLConsentContextHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/saml-consent", sessionReq, s.handleSAMLConsentDecisionHTTP)
```

- [ ] **Step 4: Write the handler test**

Create `pkg/server/handle_saml_consent_test.go`, mirroring `pkg/server/handle_consent_test.go` (same server-under-test harness, session injection, and `s.kvStore` access). Cover: valid ticket → context with the SP display name + attributes; `approve` writes a `saml_consent` row (assert via `HasSAMLConsent`) and returns `redirect == ticket.ReturnTo`; `decline` returns `"/"` and writes no row; re-using a consumed ticket → invalid-ticket error; a ticket bound to another account → invalid.

- [ ] **Step 5: Run tests & build**

Run: `go build -tags nodynamic ./... && go test ./pkg/server/ -run SAMLConsent -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/contract/saml_consent.go pkg/server/handle_saml_consent.go pkg/server/handle_saml_consent_test.go pkg/server/server.go
git commit -m "feat(saml): consent context + decision endpoints"
```

---

### Task 5: OIDC consent — incremental-grant context

**Goal:** Tell the consent screen which requested scopes the user has already granted, so it can frame re-prompts as "additional access".

**Files:**
- Modify: `pkg/contract/auth.go:363-367` (`ConsentContext`)
- Modify: `pkg/server/handle_consent.go:18-51` (`handleConsentContextHTTP`)
- Test: `pkg/server/handle_consent_test.go` (extend)

**Acceptance Criteria:**
- [ ] `GET /consent` includes `alreadyGranted` — the subset of returned `scopes` already in the stored grant (empty for a first-time consent).

**Verify:** `go test ./pkg/server/ -run Consent -v` → PASS.

**Steps:**

- [ ] **Step 1: Extend the contract**

In `pkg/contract/auth.go`, add the field to `ConsentContext`:

```go
type ConsentContext struct {
	Client  ConsentClient `json:"client"`
	Account ConsentUser   `json:"account"`
	Scopes  []string      `json:"scopes"`
	// AlreadyGranted is the subset of Scopes the account has previously consented
	// to (so the UI can mark the NEW scopes on an incremental re-consent). Empty
	// on a first-time consent.
	AlreadyGranted []string `json:"alreadyGranted,omitempty"`
}
```

- [ ] **Step 2: Populate it in the handler**

In `pkg/server/handle_consent.go`, inside `handleConsentContextHTTP`, after loading `client` and before encoding `out`, load the existing grant and set the field:

```go
	granted, gerr := s.queries.GetConsent(r.Context(), db.GetConsentParams{
		AccountID: sess.Data.AccountID, ClientID: ticket.ClientID,
	})
	if gerr != nil && !errors.Is(gerr, pgx.ErrNoRows) {
		writeAuthErr(w, gerr)
		return
	}
	// granted is nil on ErrNoRows (first-time consent).
	out.AlreadyGranted = granted
```

(`errors`, `pgx`, and `db` are already imported in `handle_consent.go`.)

- [ ] **Step 3: Extend the test**

In `pkg/server/handle_consent_test.go`, add a case: pre-seed a consent grant for `[openid]`, mint a ticket requesting `[openid, email]`, GET the context, assert `AlreadyGranted == ["openid"]` and `Scopes == ["openid","email"]`. Add a first-time case asserting `AlreadyGranted` is empty.

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/server/ -run Consent -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/contract/auth.go pkg/server/handle_consent.go pkg/server/handle_consent_test.go
git commit -m "feat(oidc): consent context reports already-granted scopes"
```

---

### Task 6: Unify `/me/consent` + revoke by kind

**Goal:** Return OIDC consents and SAML acks in one list tagged by `kind`, and route revoke to the right table.

**Files:**
- Modify: `pkg/contract/launchpad.go` (`ConsentedApp.Kind`, `RevokeConsentInput.Kind`)
- Modify: `pkg/server/handle_me_consent.go` (merge + revoke-by-kind)
- Test: `pkg/server/handle_me_consent_test.go` (extend)

**Acceptance Criteria:**
- [ ] `GET /me/consent` returns OIDC entries (`kind:"oidc"`) and SAML entries (`kind:"saml"`, `clientId` = SP id string), sorted by name.
- [ ] `POST /me/consent/revoke {kind:"saml", clientId:"<spId>"}` deletes the SAML ack; default/`"oidc"` deletes the OIDC consent; both idempotent.

**Verify:** `go test ./pkg/server/ -run MyConsent -v` → PASS.

**Steps:**

- [ ] **Step 1: Extend the contracts**

In `pkg/contract/launchpad.go`, add `Kind` to both DTOs:

```go
type ConsentedApp struct {
	Kind      string    `json:"kind"` // "oidc" | "saml"
	ClientID  string    `json:"clientId"`
	Name      string    `json:"name"`
	IconURL   *string   `json:"iconUrl,omitempty"`
	Scopes    []string  `json:"scopes"`
	GrantedAt time.Time `json:"grantedAt"`
}

type RevokeConsentInput struct {
	Kind     string `json:"kind,omitempty"` // "oidc" (default) | "saml"
	ClientID string `json:"clientId"`
}
```

- [ ] **Step 2: Merge SAML acks into the list**

In `pkg/server/handle_me_consent.go`, extend the `consentMgmtQueries` interface and `listConsents`:

```go
type consentMgmtQueries interface {
	ListConsentsByAccount(ctx context.Context, accountID int32) ([]db.ListConsentsByAccountRow, error)
	DeleteConsent(ctx context.Context, arg db.DeleteConsentParams) error
	ListSAMLConsentsByAccount(ctx context.Context, accountID int32) ([]db.ListSAMLConsentsByAccountRow, error)
	DeleteSAMLConsent(ctx context.Context, arg db.DeleteSAMLConsentParams) error
	GetEntityIconEtag(ctx context.Context, arg db.GetEntityIconEtagParams) (string, error)
}
```

Rewrite `listConsents` to tag OIDC and append SAML, then sort by name:

```go
func (s *Server) listConsents(ctx context.Context, accountID int32) ([]contract.ConsentedApp, error) {
	q := s.getConsentMgmtQueries()

	oidc, err := q.ListConsentsByAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("listConsents oidc: %w", err)
	}
	out := make([]contract.ConsentedApp, 0, len(oidc))
	for _, r := range oidc {
		var iconURL *string
		if etag, e := q.GetEntityIconEtag(ctx, db.GetEntityIconEtagParams{OwnerKind: "oidc_client", OwnerID: r.ClientID}); e == nil {
			iconURL = entityIconURLPtr("oidc_client", r.ClientID, etag)
		}
		out = append(out, contract.ConsentedApp{
			Kind: "oidc", ClientID: r.ClientID, Name: r.DisplayName, IconURL: iconURL,
			Scopes: append([]string(nil), r.GrantedScopes...), GrantedAt: r.UpdatedAt.Time,
		})
	}

	saml, err := q.ListSAMLConsentsByAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("listConsents saml: %w", err)
	}
	for _, r := range saml {
		id := strconv.FormatInt(r.SpID, 10)
		var iconURL *string
		if etag, e := q.GetEntityIconEtag(ctx, db.GetEntityIconEtagParams{OwnerKind: "saml_sp", OwnerID: id}); e == nil {
			iconURL = entityIconURLPtr("saml_sp", id, etag)
		}
		out = append(out, contract.ConsentedApp{
			Kind: "saml", ClientID: id, Name: r.DisplayName, IconURL: iconURL,
			Scopes: nil, GrantedAt: r.UpdatedAt.Time,
		})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
```

Add `"sort"` and `"strconv"` to the import block.

- [ ] **Step 3: Route revoke by kind**

Replace `revokeConsent` and update `handleRevokeMyConsent` to dispatch:

```go
func (s *Server) revokeConsent(ctx context.Context, accountID int32, kind, id string) error {
	q := s.getConsentMgmtQueries()
	if kind == "saml" {
		spID, perr := strconv.ParseInt(id, 10, 64)
		if perr != nil {
			return fmt.Errorf("revokeConsent: bad saml id %q: %w", id, perr)
		}
		if err := q.DeleteSAMLConsent(ctx, db.DeleteSAMLConsentParams{AccountID: accountID, SpID: spID}); err != nil {
			return fmt.Errorf("revokeConsent saml: %w", err)
		}
		return nil
	}
	if err := q.DeleteConsent(ctx, db.DeleteConsentParams{AccountID: accountID, ClientID: id}); err != nil {
		return fmt.Errorf("revokeConsent oidc: %w", err)
	}
	return nil
}
```

In `handleRevokeMyConsent`, pass the kind through (default empty → OIDC):

```go
	if err := s.revokeConsent(ctx, sess.Account.ID, in.Body.Kind, in.Body.ClientID); err != nil {
		return nil, err
	}
```

- [ ] **Step 4: Extend the test**

In `pkg/server/handle_me_consent_test.go`, update the fake `consentMgmtQueries` to implement the two new methods, and add cases: list returns merged OIDC+SAML sorted by name with correct `kind`; `revoke {kind:"saml", clientId:"42"}` calls `DeleteSAMLConsent(42)`; `revoke {clientId:"app"}` (no kind) still deletes the OIDC consent.

- [ ] **Step 5: Run tests**

Run: `go test ./pkg/server/ -run 'MyConsent|Consent' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/contract/launchpad.go pkg/server/handle_me_consent.go pkg/server/handle_me_consent_test.go
git commit -m "feat(consent): unify /me/consent across oidc + saml; revoke by kind"
```

---

### Task 7: Frontend — shared ConsentCard + OIDC screen maturity

**Goal:** Factor a shared consent card and add the "remembered next time" reassurance + the incremental "additional access" highlight to the OIDC screen.

**Files:**
- Create: `dashboard/src/components/custom/ConsentCard.vue`
- Modify: `dashboard/src/pages/ConsentView.vue`
- Modify: `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`
- Test: `dashboard/src/pages/ConsentView.test.ts` (create/extend)

**Acceptance Criteria:**
- [ ] First-time consent shows the standard scope list + reassurance line.
- [ ] When `alreadyGranted` is non-empty, the screen titles "additional access" and marks scopes not in `alreadyGranted` as new.
- [ ] en/zh parity holds (`locales.parity.test.ts`).

**Verify:** `cd dashboard && npm run test -- ConsentView` and `npm run test -- locales` → PASS; `npm run build` typechecks.

**Steps:**

- [ ] **Step 1: Add i18n strings**

In `dashboard/src/locales/en.ts`, inside the existing `consent:` block, add:

```ts
    remembered: "You're approving this once — next time {client} signs you in automatically, unless it asks for new permissions.",
    manageHint: 'You can review or revoke this anytime in Settings → App access.',
    additionalAccessTitle: '{client} is requesting additional access',
    newBadge: 'New',
```

In `dashboard/src/locales/zh.ts`, add the parity keys:

```ts
    remembered: '你只需批准一次 —— 下次 {client} 会自动让你登录，除非它申请新的权限。',
    manageHint: '你可以随时在 设置 → 应用访问 中查看或撤销。',
    additionalAccessTitle: '{client} 申请额外的访问权限',
    newBadge: '新增',
```

- [ ] **Step 2: Create the shared card**

Create `dashboard/src/components/custom/ConsentCard.vue` — the logo + heading + account line + body slot + actions slot + policy/ToS footer, extracted from the current `ConsentView` markup:

```vue
<script setup lang="ts">
defineProps<{
  logoUri?: string
  displayName: string
  accountName: string
  policyUri?: string
  tosUri?: string
}>()
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
</script>

<template>
  <div class="flex flex-col gap-6">
    <div class="flex flex-col items-center gap-2 text-center">
      <img v-if="logoUri" :src="logoUri" :alt="displayName" class="size-12 rounded-md object-contain" />
      <slot name="heading" />
      <p class="text-sm text-muted">{{ t('consent.yourAccount', { displayName: accountName }) }}</p>
    </div>

    <slot name="body" />
    <slot name="actions" />

    <p v-if="policyUri || tosUri" class="text-center text-xs text-muted">
      <a v-if="policyUri" :href="policyUri" target="_blank" rel="noopener noreferrer" class="underline-offset-4 hover:underline">{{ t('consent.privacyPolicy') }}</a>
      <span v-if="policyUri && tosUri"> &middot; </span>
      <a v-if="tosUri" :href="tosUri" target="_blank" rel="noopener noreferrer" class="underline-offset-4 hover:underline">{{ t('consent.termsOfService') }}</a>
    </p>
  </div>
</template>
```

- [ ] **Step 3: Rebuild `ConsentView` on the card**

Modify `dashboard/src/pages/ConsentView.vue`: extend the `ConsentContext` interface with `alreadyGranted?: string[]`; compute `isIncremental` and `newScopes`; render via `ConsentCard`. Replace the `<div v-else-if="ctx" …>` block with:

```vue
    <ConsentCard
      v-else-if="ctx"
      :logo-uri="ctx.client.logoUri"
      :display-name="ctx.client.displayName"
      :account-name="ctx.account.displayName"
      :policy-uri="ctx.client.policyUri"
      :tos-uri="ctx.client.tosUri"
    >
      <template #heading>
        <p class="text-ink">
          {{ isIncremental
            ? t('consent.additionalAccessTitle', { client: ctx.client.displayName })
            : t('consent.requestingAccess', { client: ctx.client.displayName }) }}
        </p>
      </template>
      <template #body>
        <div class="flex flex-col gap-2">
          <p class="text-sm font-medium text-ink">{{ t('consent.scopesHeading') }}</p>
          <ConsentScopeList :scopes="ctx.scopes" :new-scopes="newScopes" />
          <p class="text-xs text-muted">{{ t('consent.remembered', { client: ctx.client.displayName }) }}</p>
          <p class="text-xs text-muted">{{ t('consent.manageHint') }}</p>
        </div>
        <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ errorText }}</AlertDescription>
        </Alert>
      </template>
      <template #actions>
        <div class="flex gap-3">
          <Button variant="outline" class="flex-1" :disabled="busy" @click="decide('deny')">{{ t('consent.deny') }}</Button>
          <Button class="flex-1" :disabled="busy" @click="decide('approve')">{{ t('consent.approveCount', { count: ctx.scopes.length }) }}</Button>
        </div>
      </template>
    </ConsentCard>
```

Add to `<script setup>`:

```ts
import { computed } from 'vue'
import ConsentCard from '@/components/custom/ConsentCard.vue'
// interface ConsentContext gains: alreadyGranted?: string[]
const isIncremental = computed(() => (ctx.value?.alreadyGranted?.length ?? 0) > 0)
const newScopes = computed(() => {
  const had = new Set(ctx.value?.alreadyGranted ?? [])
  return (ctx.value?.scopes ?? []).filter((s) => !had.has(s))
})
```

- [ ] **Step 4: Mark new scopes in `ConsentScopeList`**

Modify `dashboard/src/components/custom/ConsentScopeList.vue`: accept an optional `newScopes?: string[]` prop; when a scope is in `newScopes`, render a small `{{ t('consent.newBadge') }}` pill next to it. (Read the current file first; keep its existing per-scope markup and only add the badge.)

- [ ] **Step 5: Write/extend the test**

Create `dashboard/src/pages/ConsentView.test.ts` (mirror an existing page test for mount + i18n + mocked `api`): mock `GET /consent` to return `{client, account, scopes:['openid','email'], alreadyGranted:['openid']}`; assert the heading uses `additionalAccessTitle` and only `email` carries the New badge. Add a first-time case (`alreadyGranted` absent) asserting the standard heading and no badge.

- [ ] **Step 6: Run tests**

Run: `cd dashboard && npm run test -- ConsentView locales`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add dashboard/src/components/custom/ConsentCard.vue dashboard/src/components/custom/ConsentScopeList.vue dashboard/src/pages/ConsentView.vue dashboard/src/pages/ConsentView.test.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(ui): consent card + remembered/incremental messaging"
```

---

### Task 8: Frontend — SAML advisory consent screen

**Goal:** A `/saml-consent` page that renders the advisory acknowledgement and posts the decision.

**Files:**
- Create: `dashboard/src/pages/SamlConsentView.vue`
- Modify: `dashboard/src/router/index.ts` (add the public route)
- Modify: `dashboard/src/locales/en.ts`, `zh.ts` (+ `title.samlConsent`)
- Test: `dashboard/src/pages/SamlConsentView.test.ts`

**Acceptance Criteria:**
- [ ] Renders the SP name, the attributes list (or a generic fallback when empty), the reassurance line, and Continue / Not now buttons.
- [ ] Continue posts `decision:'approve'`; Not now posts `decision:'decline'`; both `hardRedirect` to the returned URL.

**Verify:** `cd dashboard && npm run test -- SamlConsentView locales` → PASS; `npm run build` typechecks.

**Steps:**

- [ ] **Step 1: Add i18n strings**

In `dashboard/src/locales/en.ts`, add a `samlConsent` block and a `title.samlConsent`:

```ts
  samlConsent: {
    title: 'Sign in to {sp}',
    intro: '{sp} will use your account to sign you in.',
    receives: '{sp} will receive:',
    genericAttributes: 'your profile information',
    remembered: "You're acknowledging this once — next time you'll be signed in automatically, unless you revoke it in Settings → App access.",
    continue: 'Continue',
    decline: 'Not now',
  },
```

In `dashboard/src/locales/zh.ts`:

```ts
  samlConsent: {
    title: '登录到 {sp}',
    intro: '{sp} 将使用你的账户为你登录。',
    receives: '{sp} 将获得：',
    genericAttributes: '你的个人资料信息',
    remembered: '你只需确认一次 —— 下次将自动登录，除非你在 设置 → 应用访问 中撤销。',
    continue: '继续',
    decline: '暂不',
  },
```

Add `samlConsent: '登录'` / `'Sign in'`-style entries to the existing `title:` blocks (`title.samlConsent`).

- [ ] **Step 2: Create the page**

Create `dashboard/src/pages/SamlConsentView.vue` (modeled on `ConsentView.vue`, reusing `ConsentCard`):

```vue
<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, type ApiError } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { hardRedirect } from '@/lib/navigate'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import ConsentCard from '@/components/custom/ConsentCard.vue'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import CardSkeleton from '@/components/custom/CardSkeleton.vue'

interface SamlConsentContext {
  sp: { id: string; displayName: string; logoUri?: string }
  account: { displayName: string }
  attributes: string[]
}
interface ConsentResult { redirect: string }

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { busy, run, errorText } = useApi()

const ticket = String(route.query.ticket ?? '')
const ctx = ref<SamlConsentContext | null>(null)
const loading = ref(true)
const hasAttrs = computed(() => (ctx.value?.attributes.length ?? 0) > 0)

onMounted(async () => {
  try {
    ctx.value = await api.get<SamlConsentContext>(`/api/prohibitorum/saml-consent?ticket=${encodeURIComponent(ticket)}`)
  } catch (e) {
    const code = (e as ApiError | undefined)?.code
    if (code === 'no_session') router.replace({ name: 'login', query: { return_to: route.fullPath } })
    else router.replace({ name: 'error', query: { error: code ?? 'invalid_consent_ticket' } })
  } finally {
    loading.value = false
  }
})

async function decide(decision: 'approve' | 'decline'): Promise<void> {
  const res = await run(() => api.post<ConsentResult>('/api/prohibitorum/saml-consent', { ticket, decision }))
  if (!res) return
  hardRedirect(res.redirect)
}
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">{{ ctx ? t('samlConsent.title', { sp: ctx.sp.displayName }) : t('samlConsent.title', { sp: '' }) }}</h1>
    </template>

    <CardSkeleton v-if="loading" :lines="3" />

    <ConsentCard
      v-else-if="ctx"
      :logo-uri="ctx.sp.logoUri"
      :display-name="ctx.sp.displayName"
      :account-name="ctx.account.displayName"
    >
      <template #heading>
        <p class="text-ink">{{ t('samlConsent.intro', { sp: ctx.sp.displayName }) }}</p>
      </template>
      <template #body>
        <div class="flex flex-col gap-2">
          <p class="text-sm font-medium text-ink">{{ t('samlConsent.receives', { sp: ctx.sp.displayName }) }}</p>
          <ul v-if="hasAttrs" class="list-disc pl-5 text-sm text-ink">
            <li v-for="a in ctx.attributes" :key="a">{{ a }}</li>
          </ul>
          <p v-else class="text-sm text-ink">{{ t('samlConsent.genericAttributes') }}</p>
          <p class="text-xs text-muted">{{ t('samlConsent.remembered') }}</p>
        </div>
        <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ errorText }}</AlertDescription>
        </Alert>
      </template>
      <template #actions>
        <div class="flex gap-3">
          <Button variant="outline" class="flex-1" :disabled="busy" @click="decide('decline')">{{ t('samlConsent.decline') }}</Button>
          <Button class="flex-1" :disabled="busy" @click="decide('approve')">{{ t('samlConsent.continue') }}</Button>
        </div>
      </template>
    </ConsentCard>
  </CenteredLayout>
</template>
```

- [ ] **Step 3: Register the route**

In `dashboard/src/router/index.ts`, add alongside the other public threshold routes (e.g. after `/consent`):

```ts
  {
    path: '/saml-consent',
    name: 'saml-consent',
    component: () => import('../pages/SamlConsentView.vue'),
    meta: { public: true, titleKey: 'title.samlConsent' },
  },
```

- [ ] **Step 4: Write the test**

Create `dashboard/src/pages/SamlConsentView.test.ts` (mirror `ConsentView.test.ts`): mock `GET /saml-consent` → `{sp:{id:'42',displayName:'Salesforce'}, account:{displayName:'Jesse'}, attributes:['Email']}`; assert the attributes list renders `Email`; click Continue → asserts `api.post` called with `{ticket, decision:'approve'}` and `hardRedirect` invoked. Add an empty-attributes case asserting the generic fallback text.

- [ ] **Step 5: Run tests**

Run: `cd dashboard && npm run test -- SamlConsentView locales`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/pages/SamlConsentView.vue dashboard/src/pages/SamlConsentView.test.ts dashboard/src/router/index.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(ui): SAML advisory consent screen + route"
```

---

### Task 9: Frontend — App access lists/revokes both kinds

**Goal:** Show OIDC consents and SAML acks together on the App-access page, with a kind label and a working revoke for both.

**Files:**
- Modify: `dashboard/src/pages/AppAccessView.vue`
- Modify: `dashboard/src/locales/en.ts`, `zh.ts`
- Test: `dashboard/src/pages/AppAccessView.test.ts` (extend)

**Acceptance Criteria:**
- [ ] Both kinds render; OIDC rows show scopes, SAML rows show a "Signs you in" descriptor.
- [ ] Revoke sends `{kind, clientId}` and refreshes; en/zh parity holds.

**Verify:** `cd dashboard && npm run test -- AppAccessView locales` → PASS.

**Steps:**

- [ ] **Step 1: Read the current page**

Run: `sed -n '1,200p' dashboard/src/pages/AppAccessView.vue`
Understand its existing list/revoke shape (it consumes `GET /me/consent` and `POST /me/consent/revoke`).

- [ ] **Step 2: Add i18n strings**

In `en.ts` (inside `appAccess`): `kindOidc: 'OIDC', kindSaml: 'SAML', samlDescriptor: 'Signs you in to this app'`. In `zh.ts`: `kindOidc: 'OIDC', kindSaml: 'SAML', samlDescriptor: '用于登录此应用'`.

- [ ] **Step 3: Update the view**

Add `kind` to the row interface; for `kind === 'saml'` render `t('appAccess.samlDescriptor')` instead of the scopes list; pass `kind` in the revoke call:

```ts
await api.post('/api/prohibitorum/me/consent/revoke', { kind: app.kind, clientId: app.clientId })
```

Key the list by `${app.kind}:${app.clientId}` (SAML and OIDC ids can collide numerically).

- [ ] **Step 4: Extend the test**

In `dashboard/src/pages/AppAccessView.test.ts`, mock `GET /me/consent` returning one `oidc` and one `saml` entry; assert both render, the SAML row shows the descriptor (no scopes), and revoking the SAML row posts `{kind:'saml', clientId:'42'}`.

- [ ] **Step 5: Run tests**

Run: `cd dashboard && npm run test -- AppAccessView locales`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/pages/AppAccessView.vue dashboard/src/pages/AppAccessView.test.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(ui): App access lists + revokes oidc and saml"
```

---

### Task 10: Smoke arc — SAML advisory consent

**Goal:** End-to-end coverage: first SSO bounces to consent context → approve records the ack → SSO issues → `/me/consent` shows it (kind saml) → revoke → re-prompt.

**Files:**
- Modify: `cmd/smoke/` (the SAML arc; follow the existing per-arc numbered local-count style)

**Acceptance Criteria:**
- [ ] A new `saml-consent` arc runs green under `SMOKE_EXIT=0`.

**Verify:** Live smoke per memory `live-smoke-without-podman` → `SMOKE_EXIT=0`.

**Steps:**

- [ ] **Step 1: Read the existing SAML smoke arc**

Run: `grep -rn "saml" cmd/smoke/*.go | grep -i "arc\|sso\|func"` and read the closest arc to mirror its helpers (session cookie jar, SP setup, `IsAccountAuthorizedForSAMLSP` grant).

- [ ] **Step 2: Add the arc**

Extend the SAML arc: after granting access, `GET /saml/sso/init?sp=<entity>` (follow-redirects OFF) → assert `302` to `/saml-consent?ticket=`; extract the ticket; `GET /api/prohibitorum/saml-consent?ticket=` → assert SP display name; `POST` `{ticket, decision:"approve"}` → assert `redirect` is the original init URL; re-`GET` the init URL → assert the auto-POST form (`SAMLResponse`); `GET /me/consent` → assert an entry with `kind:"saml"`; `POST /me/consent/revoke {kind:"saml", clientId}` → `GET /me/consent` no longer lists it. Number lines in the existing `saml-consent N/M` style.

- [ ] **Step 3: Run live smoke**

Run the throwaway-cluster smoke per the `live-smoke-without-podman` memory.
Expected: `SMOKE_EXIT=0`, the `saml-consent` arc green.

- [ ] **Step 4: Commit**

```bash
git add cmd/smoke/
git commit -m "test(smoke): SAML advisory consent arc"
```

---

### Task 11: Rebuild SPA bundle + full green gate

**Goal:** Ship the embedded bundle and prove the whole gate is green.

**Files:**
- Modify (generated): `pkg/webui/dist/*`

**Acceptance Criteria:**
- [ ] Backend + frontend + smoke all green; embedded bundle rebuilt and committed.

**Verify:** the commands below, all green.

**Steps:**

- [ ] **Step 1: Frontend gate + build**

```bash
cd dashboard && npm test && npm run build
```
Expected: tests PASS; `vue-tsc` clean; `vite build` writes `../pkg/webui/dist`.

- [ ] **Step 2: Backend gate**

```bash
cd /Users/tundra/go/src/tundra/Prohibitorum
go build -tags nodynamic ./... && go vet ./... && go test ./...
```
Expected: PASS.

- [ ] **Step 3: Live smoke**

Run the throwaway-cluster smoke (memory `live-smoke-without-podman`); expect `SMOKE_EXIT=0`.

- [ ] **Step 4: Commit the bundle**

```bash
git add pkg/webui/dist
git commit -m "chore(consent): rebuild embedded SPA bundle"
```

---

## Self-review

**Spec coverage:**
- A. OIDC maturity → Tasks 5 (already-granted) + 7 (reassurance + incremental highlight). ✓
- B. SAML advisory consent → Tasks 1 (table/queries), 2 (ticket), 3 (gate + interposition, both flows), 4 (endpoints), 8 (screen). ✓ (decline→`/` in Task 4; attributes shown in Tasks 3/4/8; remembered-until-revoked = no re-prompt logic beyond row existence.) ✓
- C. Unify management → Task 6 (`/me/consent` + revoke by kind) + Task 9 (App access UI). ✓
- Security (no signed-query exposure via server-held `ReturnTo`; gate after RBAC; idempotent revoke) → Tasks 2, 3, 4, 6. ✓
- Testing & gate → Tasks 3,4,5,6 (Go), 7,8,9 (vitest), 10 (smoke), 11 (full gate). ✓
- Forward-auth untouched. ✓

**Type consistency:** `SAMLConsentTicket` (authn) ↔ `db.HasSAMLConsentParams/UpsertSAMLConsentParams/DeleteSAMLConsentParams` (int32 account, int64 sp) ↔ `contract.SAMLConsentContext/SAMLConsentDecision` ↔ `ConsentedApp.Kind`/`RevokeConsentInput.Kind`. `maybeDemandSAMLConsent(... accountID int32, sp db.SamlSp)` matches both call sites. OIDC `ConsentContext.AlreadyGranted` ↔ `ConsentView` `alreadyGranted?` ↔ `ConsentScopeList` `newScopes`. ✓

**Open verification points flagged inline (not placeholders):** `migrations.go` discovery style (Task 1 Step 2); `req.SP` concrete type (Task 3 Step 3 note); `kv.NewMemory` constructor name (Task 2 Step 1); `attrMapEntry` JSON tags (Task 3 Step 2). Each has a concrete fallback instruction.
