# Handover — Backend config correctness + completeness audit (OIDC upstream, OIDC/SAML downstream)

**Date:** 2026-06-10. **Branch:** `master` (commit directly; no remote). **HEAD:** `f4b8d0a`. **Tree:** clean.
Read project memory first (`MEMORY.md` + `project_current_state.md` + `reference_backend_backlog.md` +
`reference_reka_primitive_idioms.md` + `feedback_doc_writing_anchor_to_code.md`), then this note.

## ▶ THE TASK
Audit the **backend logic** for **correctness** and **completeness**, driven by the configuration surface
the admin frontend exposes for the three protocol integrations: **OIDC upstream federation**, **OIDC
downstream (OP)**, and **SAML downstream (IdP)**. These have many fields/toggles. For each, answer:

1. **Correctness** — does the backend actually implement the field's stated behavior, and correctly?
2. **Completeness — two directions:**
   - **FE → backend:** is every FE-configurable field fully wired end-to-end, or stored-but-unused
     (a "no-op", like the two we already found this session)?
   - **backend → FE:** are there DB columns / backend capabilities that ARE wired but NOT exposed in
     the FE (a missing control), or columns that are dead (stored, never read)?
3. **Classify each finding:** ✅ correct & wired · ⚠️ wired but FE description/label imprecise ·
   🔴 no-op (FE control or DB column does nothing) · 🟡 backend capability not exposed in FE ·
   ⛔ genuinely missing/broken backend behavior the FE implies exists.

Deliverable: an audit report (write to `docs/superpowers/notes/2026-06-10-backend-config-audit-FINDINGS.md`)
with a row per field, the verdict, the code citation, and a recommended action. This is **read-and-verify
work first**; only propose/implement fixes after the report is reviewed (some "gaps" are deliberately
deferred — cross-check `reference_backend_backlog`).

## Method
- For each config table column, find (a) where it is WRITTEN (admin create/update handler + sqlc), and
  (b) where it is READ at runtime (the protocol flow). A column written but never read at runtime is a
  no-op. Cite `file:line` for both (the lesson `feedback_doc_writing_anchor_to_code`: anchor every claim
  to code; do NOT trust labels/descriptions).
- `go build ./... && go vet ./...` exit 0 is authoritative. The smoke (`/tmp/run_v06.sh` →
  `/tmp/v06.result`, poll `SMOKE_EXIT=`) exercises most flows end-to-end against a live DB and is the best
  "is it really wired" oracle — grep `cmd/smoke/main.go` to see which fields it already proves.
- Watch for: stored-but-unread columns; gates that apply on one path but not another (e.g. re-login skips
  most gates); anti-enumeration error collapsing; FE labels that overclaim.

## Already verified THIS session (precision pass — transcribe into the report as starting evidence)
A description-precision pass on 2026-06-10 read most of the relevant backend with citations. Key findings:

### OIDC upstream — `pkg/federation/oidc/{modes.go,federation.go,client.go}`, `pkg/server/handle_admin_upstream_idps.go`, `handle_federation.go`, `handle_me_identities.go`
- **`Resolve` (modes.go:54)**: existing `account_identity` for (iss,sub) → re-login via `syncClaims`, **no
  disabled / verified-email / domain checks**. New identity → dispatch on `mode`.
- **`mode=auto_provision`** (modes.go:121): gates `require_verified_email` (modes.go:140), `allowed_domains`
  (modes.go:147, empty=any), username-present, username-collision (tx); creates the account.
- **`mode=invite_only`** (modes.go:284): requires an enrollment token; `ConsumeEnrollment` + create from the
  invite TEMPLATE. **Skips** verified-email + domains by design (modes.go:281). No token → `invite_required`.
- **`mode=link_only`** (modes.go:499): `applyLinkOnly` **always rejects** an unlinked identity
  (`link_required`). It NEVER creates accounts. Linking happens via the authenticated self-service
  `LinkCallback` (federation.go:~427), which is **mode-independent** and DOES apply verified-email
  (federation.go:459) + domain (federation.go:474) gates.
- **`disabled`**: excluded from the public `GET /auth/federation` list (`ListUpstreamIDPs WHERE NOT
  disabled`); blocks new login + link (`GetUpstreamIDPBySlug WHERE NOT disabled`); **but re-login of an
  already-linked identity is NOT blocked** (Resolve has no disabled check). `CountUsableSignInFederation`
  excludes disabled (used by the last-method unlink gate).
- **claim fields** `username_claim/display_name_claim/email_claim`: read via `ClaimString(raw, claim)`
  (client.go:75); schema defaults preferred_username/name/email. **`scopes`**: sent in the upstream
  authorize request (client.go:125); per-IdP client cache (15min TTL).
- **DB columns** (`upstream_idp`): all FE-exposed EXCEPT `KeyVersion`, `ClientSecretEnc`, `SecretNonce`
  (internal secret-rotation/sealing). → likely complete; verify no dead columns.

### OIDC downstream — `pkg/protocol/oidc/{authorize.go,client.go,token*,introspect.go,revoke.go,logout.go,clientgen.go}`, `pkg/server/handle_admin_oidc_clients.go`
- **`public` / `token_endpoint_auth_method=none`** (clientgen.go:69): no secret; `authenticateClient`
  rejects secret auth (client.go:148); introspect refused for public clients (introspect.go:39). **PKCE is
  required for ALL clients** (`RequirePkce` always true, clientgen.go:62; enforced authorize.go:92).
- **`require_consent`** (authorize.go:197): false → consent **skipped entirely** (trusted). true → consent
  shown UNLESS a remembered grant covers the scopes (authorize.go:207) or `prompt=consent` (authorize.go:206).
- **`disabled`** (`GetOIDCClient WHERE disabled=false`, client.go:50): blocks authorize/token/introspect/
  revoke; **existing tokens are NOT revoked** (stay valid to expiry).
- **`redirectUris` / `postLogoutRedirectUris`**: **exact-match** allowlists (`slices.Contains`,
  authorize.go:64, logout.go:91). **`allowedScopes`**: request scopes must be a subset; `openid` required
  (authorize.go:77).
- **🟡 UNEXPOSED `oidc_client` columns — AUDIT EACH (wired or dead?):** `RequirePkce` (always true),
  `AllowedCodeChallengeMethods` (S256-only per v0.6 smoke), `IDTokenSignedResponseAlg`, `SubjectType`
  (pairwise? the backlog lists pairwise as deferred), `ApplicationType`, `DefaultMaxAge` + `RequireAuthTime`
  (relate to v0.6 forced-reauth / `max_age` — likely wired; confirm), `Contacts`, `LogoUri`, `TosUri`,
  `PolicyUri` (RP metadata — likely dead/cosmetic). Determine which are read at runtime vs dead, and which
  deserve an FE control.

### SAML downstream — `pkg/protocol/saml/{sso.go,authnreq.go,assertion.go,attributes.go,subjectid.go,sso_init.go,metadata.go}`, `pkg/server/handle_admin_saml_sps.go`
- **`require_signed_authn_request`** (authnreq.go:152): rejects unsigned / bad-signature AuthnRequests. ✅
- **`want_assertions_signed`** — **🔴 NO-OP**: assertions are ALWAYS signed (assertion.go:199); the column
  is never read after storage. **FE control was REMOVED this session** (user decision); the DB column +
  PUT field remain (backend still accepts). Decide if the column should be dropped or wired.
- **`allow_idp_initiated`** (sso_init.go:76): opt-in for `GET /saml/sso/init`; else 403. ✅
- **`name_id_format`** (assertion.go:163): sets the NameID Format; validated vs the AuthnRequest
  NameIDPolicy (sso.go:240, InvalidNameIDPolicy). ✅
- **`name_id_claim`** — **🔴 NO-OP**: the NameID is ALWAYS a stable random per-(account,sp) id
  (subjectid.go); the column is never applied. **FE control REMOVED this session**; column remains.
- **`attribute_map`** (attributes.go:40 `projectAttributes`): maps account fields → assertion Attributes;
  entry `{name,name_format,friendly_name,source,multi}`; source ∈ {`username`, `attributes.<key>`,
  `attributes.administrator`}. ✅ (verify multi/coercion edge cases).
- **`session_lifetime`** (assertion.go:66): `SessionNotOnOrAfter` hint; default 8h. ✅
- **`entity_id`** (authnreq.go:139): SP lookup key + assertion audience + replay-key scope. ✅
- **🟡 UNEXPOSED `saml_sp` columns — AUDIT:** `SpKind` (ghes/generic; set at `saml-sp create --kind`,
  drives the default attribute map; not editable in FE detail — intentional?), **`AuthnRequestsSigned`**
  (DISTINCT from require_signed_authn_request; set at metadata ingest; the Tier-1 cycle fixed an alias bug
  where PUT clobbered it — **is it READ anywhere at runtime?** if not, it is dead), `MetadataXml` +
  `MetadataValidUntil` + `MetadataCacheDuration` + `MetadataFetchedAt` (metadata mgmt; partly surfaced).
- **Protocol completeness (cross-check backlog, likely deferred not bugs):** assertion encryption,
  front-channel SLO to other SPs, AttributeQuery, artifact binding — `reference_backend_backlog` marks
  these deferred. Distinguish "deferred & known" from "claimed by FE but missing".

## FE config surface (what to read for the exact current field set)
- OIDC upstream: `dashboard/src/pages/admin/AdminUpstreamIdps{,Detail}View.vue` — slug, displayName,
  issuerUrl, clientId, clientSecret, **mode** (auto_provision/invite_only/link_only), scopes, allowedDomains,
  username/displayName/email claim, requireVerifiedEmail, disabled.
- OIDC downstream: `AdminOidcClient{s,Detail}View.vue` — clientId, displayName, redirectUris,
  postLogoutUris, scopes (openid/profile/email checkboxes), public, requireConsent, disabled.
- SAML downstream: `AdminSamlProviders{,Detail}View.vue` — entityId, displayName, metadata XML / manual
  ACS rows (binding/location/index/default), nameIdFormat, attributeMap, requireSignedAuthnRequest,
  allowIdpInitiated, sessionLifetime. (want_assertions_signed + nameIdClaim controls were just removed.)
- The exact admin HTTP contract: `pkg/contract/auth.go` (the create/update body structs:
  `createOIDCApplicationBody`, `updateSAMLApplicationBody`, `createIdentityProviderBody`, etc. — note the
  recent rename: paths are `/oidc-applications`, `/saml-applications`, `/identity-providers`; Go ids
  `*OIDCApplication*`/`*SAMLApplication*`/`*IdentityProvider*`; DB tables still `oidc_client`/`saml_sp`/
  `upstream_idp`). The PUT/create bodies define exactly which columns the FE can set.

## Suggested execution (fresh session)
1. Re-read this note + the memories. Optionally `git log --oneline -15` for recent context.
2. Build the column-level matrix per table: every DB column → {written by? read by? FE control?} with
   citations. The structs are in `pkg/db/models.go`; the write paths in `pkg/server/handle_admin_*`; the
   read paths in `pkg/protocol/*` and `pkg/federation/oidc/*`.
3. For each, assign a verdict (✅/⚠️/🔴/🟡/⛔) + action. Fold in the verified findings above.
4. Use the smoke as the wired-or-not oracle for the big flows; for unexposed columns, grep the runtime
   read sites directly.
5. Write `…-FINDINGS.md`. Recommend (do not yet implement) fixes; flag which are deferred-by-design.
   For the two known no-op DB columns (want_assertions_signed, name_id_claim), recommend drop-vs-wire.
6. Consider whether this warrants a follow-up implementation cycle (likely: prune dead columns, expose the
   genuinely-useful unexposed ones, fix any ⛔). That cycle would go through the normal
   brainstorm→plan→subagent-driven→gate workflow.

## Conventions / env (inherited)
- `go build/vet ./...` exit 0 authoritative over stale gopls (false-positives `MissingLitField` after
  out-of-editor `sqlc generate` — run `mise exec -- sqlc generate`). gofmt repo-wide pre-existing-dirty.
- FE: `vue-tsc -b` / `npm run build` is the real FE typecheck (not `--noEmit`). After Vue edits,
  `cd dashboard && npm run build` + `git add pkg/webui/dist` only at a done-gate. en.ts apostrophe guard.
- Smoke runner `/tmp/run_v06.sh` (poll `/tmp/v06.result` for `SMOKE_EXIT=`/`DONE`; full log
  `/tmp/smoke-v06.log`). NEVER bare `pkill -f prohibitorum` (matches the dev PG `-D /tmp/prohibitorum-pg`);
  use `pkill -f 'go-build.*/prohibitorum'` / `pkill -f 'cmd/prohibitorum'`. Dev PG (`/tmp/prohibitorum-pg`,
  :55432) healthy. `mise dev-server` + `mise dev-seed` + `mise enroll-admin -- --new` for a live look.
- This is primarily a READ/VERIFY audit — most of it touches no code. Keep the tree clean; the deliverable
  is the FINDINGS doc.

## References
- This session's precision pass (the source of the verified findings above): commits `b33f73a`/`34ebe29`
  + the description corrections in `dashboard/src/locales/en.ts`.
- `reference_backend_backlog` → `docs/superpowers/notes/2026-06-08-backend-backlog.md` (deferred items —
  distinguish deferred from broken).
- Protocol specs/handoffs: `docs/superpowers/specs/` (v0.3 federation, v0.4 OIDC OP, v0.5 SAML IdP, v0.6
  protocol-completeness) define what each field was DESIGNED to do — compare design vs implementation.
