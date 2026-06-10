# Findings — Backend config correctness + completeness audit

**Date:** 2026-06-10. **Branch:** `master`. **HEAD at audit:** `70b181b`. **Tree:** clean (read/verify only; no code changed).
**Scope:** the admin configuration surface for the three protocol integrations — OIDC upstream federation,
OIDC downstream (OP), SAML downstream (IdP) — audited for correctness and completeness in both directions
(FE→backend wired? backend column→FE exposed or dead?).

**Method:** for each DB column, located (a) the WRITE path (admin create/update body → sqlc params) and
(b) the runtime READ path (protocol flow). A column written-but-never-read is a no-op/dead column; a column
read on a path the FE can never populate is an inert capability. Every verdict below is anchored to `file:line`
(per `feedback_doc_writing_anchor_to_code`). `go build ./... && go vet ./...` clean.

## Verdict taxonomy
- ✅ correct & wired — implemented as the FE implies, read at runtime.
- ⚠️ wired but imprecise — works, but a label/description or a path asymmetry could mislead.
- 🔴 no-op / dead — FE control or DB column does nothing at runtime.
- 🟡 unexposed capability — backend reads it, but the FE/admin API can't (or doesn't) set it.
- ⛔ missing/broken — FE implies behavior the backend does not deliver. **(none found — see summary.)**

---

## Summary (headline findings)
- **Upstream IdP (OIDC federation): clean.** Every non-secret column is set by the admin API and read at
  runtime. No dead columns, no FE no-ops. One correctness *nuance* worth a doc note (re-login bypasses the
  disabled/verified-email/domain gates — ⚠️, by design).
- **OIDC downstream (OP): 6 dead columns + 3 inert.** `contacts`, `subject_type`, `application_type`,
  `id_token_signed_response_alg`, `default_max_age`, `require_auth_time` are stored but never read at runtime
  (🔴). `logo_uri`/`tos_uri`/`policy_uri` ARE read (consent screen) but can never be set (🟡 inert).
  `require_pkce` + `allowed_code_challenge_methods` are read & enforced but hardcoded/immutable (✅, by design).
- **SAML downstream (IdP): 2 confirmed no-ops (already known) + 1 dead + 4 placeholder columns.**
  `want_assertions_signed` and `name_id_claim` are no-ops (🔴, FE controls already removed this session).
  `authn_requests_signed` is fully redundant/dead (🔴). The `metadata_valid_until/cache_duration/fetched_at`
  trio is never read (🔴, metadata auto-refresh unimplemented — deferred). `metadata_xml` IS read (SLO).
- **No ⛔ found.** Nothing the FE currently exposes is missing or broken on the backend. All gaps are either
  dead-storage (prunable) or deferred-by-design (per `reference_backend_backlog`).

---

## 1. OIDC upstream federation (`upstream_idp`)

Write: `handle_admin_upstream_idps.go` create `:125-208` / update `:267-316`.
Runtime read: `pkg/federation/oidc/{modes.go,federation.go,client.go}`.

| Column | Written by FE | Read at runtime | Verdict |
|---|---|---|---|
| `slug` | create | lookup key (`GetUpstreamIDPBySlug`) | ✅ |
| `display_name` | create+update | public list / login button | ✅ |
| `issuer_url` | create+update | OIDC discovery (`client.go`) | ✅ |
| `client_id` | create+update | authorize/token request | ✅ |
| `client_secret_enc`,`secret_nonce`,`key_version` | sealed on create/rotate | decrypt at token exchange | ✅ (internal; correctly write-only in views) |
| `scopes` | create+update | sent in authorize request (`client.go:125`) | ✅ |
| `mode` | create+update | `Resolve` dispatch (`modes.go`) | ✅ |
| `allowed_domains` | create+update | `auto_provision` gate (`modes.go:147`), `LinkCallback` gate (`federation.go:474`) | ✅ |
| `username_claim`/`display_name_claim`/`email_claim` | create+update | `ClaimString` (`client.go:75`) | ✅ |
| `require_verified_email` | create+update | `auto_provision` gate (`modes.go:140`), `LinkCallback` gate (`federation.go:459`) | ✅ |
| `disabled` | update | excludes from public list + blocks new login/link (`GetUpstreamIDPBySlug WHERE NOT disabled`) | ✅ / ⚠️ (see note) |

**Correctness notes (folded from the precision pass, re-verified):**
- **`mode` semantics are correct and distinct:** `auto_provision` (`modes.go:121`) gates verified-email +
  domains then creates; `invite_only` (`modes.go:284`) requires an enrollment token and creates from the
  invite template, **deliberately skipping** verified-email/domain (`modes.go:281`); `link_only`
  (`applyLinkOnly`, `modes.go:499`) **never provisions** — an unlinked identity is always rejected
  (`link_required`). Self-service linking (`LinkCallback`, `federation.go:~427`) is mode-independent and
  *does* apply the verified-email + domain gates.
- ⚠️ **`disabled` + re-login asymmetry:** `Resolve` (`modes.go:54`) routes an *existing* `account_identity`
  straight to re-login via `syncClaims` with **no disabled / verified-email / domain re-check**. So disabling
  an IdP stops *new* logins and links but does **not** stop an already-linked user from continuing to sign in
  through it. This is defensible (disable = "no new associations") but the admin "Disabled" toggle could be
  read as a hard kill-switch. **Action:** document the semantics in the FE description; no code change needed
  unless a hard cutoff is wanted (would require a disabled check in `Resolve`).

**Completeness:** no dead columns; no FE field that fails to wire. ✅

---

## 2. OIDC downstream / OP (`oidc_client`)

Write: `handle_admin_oidc_clients.go` create `:117-148` (→ `BuildClientParams`, `clientgen.go:32`) /
update `:189-236`. Runtime read: `pkg/protocol/oidc/{authorize.go,client.go,token.go,jwt.go,introspect.go,...}`,
consent at `pkg/server/handle_consent.go`.

### 2a. Wired & correct
| Column | Read site | Verdict |
|---|---|---|
| `client_id` | `GetOIDCClient WHERE disabled=false` (`client.go:50`) | ✅ |
| `display_name` | consent screen | ✅ |
| `redirect_uris` | exact-match allowlist (`authorize.go:64`, `slices.Contains`) | ✅ |
| `post_logout_redirect_uris` | exact-match allowlist (`logout.go:91`) | ✅ |
| `allowed_scopes` | subset check, `openid` required (`authorize.go:77`) | ✅ |
| `token_endpoint_auth_method` (`none` vs `client_secret_basic`) | `authenticateClient` (`client.go:148`); introspect refused for public (`introspect.go:39`) | ✅ |
| `require_consent` | consent skipped when false (`authorize.go:197`); honored grant / `prompt=consent` (`authorize.go:206-207`) | ✅ |
| `disabled` | blocks authorize/token/introspect/revoke; existing tokens NOT revoked (live to expiry) | ✅ / ⚠️ (document the token-survival) |

### 2b. Read & enforced but immutable (PKCE-always design) — ✅ by design
| Column | Set | Read | Note |
|---|---|---|---|
| `require_pkce` | hardcoded `true` at create (`clientgen.go:62`) | enforced (`authorize.go:92`) | PKCE required for ALL clients; no FE control needed. Not a no-op. |
| `allowed_code_challenge_methods` | hardcoded `["S256"]` (`clientgen.go:63`) | validated (`authorize.go:101`) | S256-only; immutable by design. |

### 2c. 🟡 Inert — read path exists but column can never be populated
| Column | Read site | Why inert | Action |
|---|---|---|---|
| `logo_uri` | consent screen (`handle_consent.go:43`) | never in create/update body nor `BuildClientParams` → always NULL | Either expose FE controls (richer consent screen) or drop. |
| `tos_uri` | consent screen (`handle_consent.go:45`) | same | same |
| `policy_uri` | consent screen (`handle_consent.go:44`) | same | same |

These are the *only* "wire a control to light up existing behavior" candidates. Low value unless consent-screen
branding is wanted.

### 2d. 🔴 Dead — stored, never read at runtime
| Column | DB default | Set by app? | Read at runtime? | Verdict |
|---|---|---|---|---|
| `contacts` | `text[]` (NULL) | no | no | 🔴 dead |
| `subject_type` | `'public'` (`002_oidc.sql:33`) | `"public"` (`clientgen.go:64`) | no — no pairwise impl | 🔴 dead (pairwise deferred per backlog T4) |
| `application_type` | `'web'` (`002_oidc.sql:34`) | `"web"` (`clientgen.go:65`) | no | 🔴 dead |
| `id_token_signed_response_alg` | `'RS256'` (`002_oidc.sql:32`) | no | no — signing hardcoded `jose.RS256` (`jwt.go:18`) | 🔴 dead (cosmetically accurate; would be ignored if changed) |
| `default_max_age` | `int` (NULL) | no | no — only the *request* `max_age` is honored (`authorize.go:150`) | 🔴 dead (per-client default-max-age unimplemented) |
| `require_auth_time` | `false` (`002_oidc.sql:36`) | no | no — `auth_time` is ALWAYS emitted (`claims.go:94`) | 🔴 dead |

**Recommended action:** prune the six dead columns in a future migration (009) unless one is a near-term
roadmap item. `subject_type` (pairwise) is explicitly deferred (backlog T4) — keep the column, leave a comment;
the rest (`contacts`, `application_type`, `id_token_signed_response_alg`, `default_max_age`, `require_auth_time`)
have no roadmap entry and are safe to drop. No FE impact (none are exposed).

---

## 3. SAML downstream / IdP (`saml_sp`)

Write: `handle_admin_saml_sps.go` create `:150-280` (→ `BuildSPParams`, `clientgen_saml.go`) /
update `:285-346`. Runtime read: `pkg/protocol/saml/{sso.go,authnreq.go,assertion.go,attributes.go,subjectid.go,sso_init.go,metadata.go,slo.go}`.

### 3a. Wired & correct
| Column | Read site | Verdict |
|---|---|---|
| `entity_id` | SP lookup + assertion audience + replay-key scope (`authnreq.go:139`) | ✅ |
| `display_name` | admin/UI label | ✅ |
| `name_id_format` | sets NameID Format; validated vs AuthnRequest NameIDPolicy (`assertion.go:163`, `sso.go:240`) | ✅ |
| `attribute_map` | `projectAttributes` (`attributes.go:40`); source ∈ {`username`,`attributes.<key>`} | ✅ (Tier-1 fix made it editable) |
| `require_signed_authn_request` | rejects unsigned/bad-sig AuthnRequests (`authnreq.go:152`) | ✅ |
| `allow_idp_initiated` | opt-in for `GET /saml/sso/init` else 403 (`sso_init.go:76`) | ✅ |
| `session_lifetime` | `SessionNotOnOrAfter` hint, default 8h (`assertion.go:66`) | ✅ |
| `metadata_xml` | parsed for SP SLO endpoint (`slo.go:404`) | ✅ (SLO) |
| `sp_kind` | seeds default attribute map at create (`clientgen_saml.go:126-130`); echoed in admin view (`handle_admin_saml_sps.go:66`) | ✅ / 🟡 (not editable post-create — by design; see note) |

### 3b. 🔴 No-op — written/stored, never applied at runtime (FE controls already removed)
| Column | Written | Read at runtime? | Verdict |
|---|---|---|---|
| `want_assertions_signed` | create `:200` + update `:341`; echoed in `SAMLApplicationView` (`auth.go:453`) | **no** — assertions are ALWAYS signed (`assertion.go:199`) | 🔴 no-op |
| `name_id_claim` | create `"sub"` (`clientgen_saml.go:144`) + update `:344`; echoed (`auth.go:450`) | **no** — NameID is ALWAYS a stable random per-(account,sp) id (`subjectid.go`) | 🔴 no-op |

**FE controls for both were removed this session** (`b33f73a`). The DB columns + PUT fields + view fields
remain. **Recommended action (drop-vs-wire):**
- `want_assertions_signed` — **wire** is the more standards-complete option (let an SP opt out of signed
  assertions), but signing-always is a safe default and the backlog doesn't list it. Recommend **drop the
  column + PUT field + view field** unless unsigned-assertion support is on the roadmap. If kept, at minimum
  stop accepting it in the PUT body to avoid implying it does something.
- `name_id_claim` — the design intentionally uses opaque per-SP pseudonyms (privacy). The "claim-driven
  NameID" idea conflicts with that. Recommend **drop the column + PUT field + view field**.

### 3c. 🔴 Dead — redundant / unimplemented
| Column | Written | Read at runtime? | Verdict |
|---|---|---|---|
| `authn_requests_signed` | create `= require_signed_authn_request` (`clientgen_saml.go:146`); **not** in `SAMLApplicationView` | **no** — IdP metadata `WantAuthnRequestsSigned` is hardcoded `true` (`metadata.go:37`), not derived from this column | 🔴 dead (fully redundant with `require_signed_authn_request`) |
| `metadata_valid_until` | metadata ingest | no | 🔴 dead (placeholder) |
| `metadata_cache_duration` | metadata ingest | no | 🔴 dead (placeholder) |
| `metadata_fetched_at` | metadata ingest | no | 🔴 dead (placeholder) |

The `metadata_*` trio supports SP-metadata auto-refresh, which is **not implemented** (metadata is ingested
once at create/reingest; never re-fetched on a TTL). Cross-check `reference_backend_backlog`: SP metadata
refresh is a deferred protocol-completeness item, so **keep these columns** (they're the schema for a planned
feature) but note they're inert today. `authn_requests_signed` has no such future use — it duplicates
`require_signed_authn_request` and the metadata advertisement ignores it — **safe to drop**.

**`sp_kind` note (🟡):** set only at create (`--kind ghes|generic`), drives which default attribute map is
seeded, then echoed read-only. It is *not* editable in the FE detail view. This is intentional (changing kind
post-create wouldn't retroactively reshape a customized attribute map), so 🟡 not ⛔ — but if operators expect
to "re-template" an SP, that's a UX gap, not a backend bug.

---

## 4. Protocol-completeness items (deferred, NOT broken)
Per `reference_backend_backlog` (T4), the following are *designed-absent*, not FE-implied-but-missing — so they
are **not** ⛔:
- SAML: assertion encryption, front-channel SLO to other SPs, AttributeQuery, artifact binding, SP-metadata
  auto-refresh (the `metadata_*` columns above).
- OIDC OP: PAR/JAR/DPoP/mTLS/DCR, pairwise subject (`subject_type` column above), device flow.
- The FE exposes none of these, so there is no overclaim. ✅

---

## 5. Recommended follow-up cycle (do NOT implement yet — for review)
A small **schema-pruning + honesty** cycle (normal brainstorm→plan→subagent→gate), roughly:
1. **Migration 009 — drop dead columns** that have no roadmap: `oidc_client.{contacts, application_type,
   id_token_signed_response_alg, default_max_age, require_auth_time}`; `saml_sp.authn_requests_signed`.
   (Keep `oidc_client.subject_type` and `saml_sp.metadata_*` — deferred-feature schema.)
2. **Drop the two SAML no-op fields end-to-end:** remove `want_assertions_signed` + `name_id_claim` from the
   PUT body, `UpdateSAMLSP` params, `SAMLApplicationView`, and the column (decision: drop, not wire).
3. **Decide `logo_uri`/`tos_uri`/`policy_uri`:** either add FE controls (consent-screen branding) or drop.
   Lowest priority.
4. **Doc-only:** clarify the upstream-IdP `disabled` re-login semantics and the OIDC-client `disabled`
   token-survival semantics in the FE descriptions (⚠️ items §1, §2a).

No ⛔ items means there is **no urgent correctness fix** — this is cleanup + truth-in-labeling work.

---

## Appendix — verification commands used
```
grep -rn '\.<Column>' pkg                 # read-site trace per column (non-generated)
pkg/protocol/oidc/clientgen.go:62-65      # OIDC create-time column defaults
pkg/protocol/oidc/jwt.go:18               # ID token signing hardcoded jose.RS256
pkg/protocol/saml/metadata.go:37          # wantSigned := true (hardcoded)
db/migrations/002_oidc.sql:32-40          # oidc_client column DB defaults
db/migrations/005_saml.sql:16-19          # saml_sp metadata_* columns
pkg/server/handle_admin_{oidc_clients,saml_sps,upstream_idps}.go  # create/update bodies
pkg/contract/auth.go:395-478              # admin read-projection views
```
