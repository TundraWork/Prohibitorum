# Backend backlog — consolidated review (2026-06-08)

Single deduplicated, prioritized inventory of everything left on the **backend** side,
gathered from: project memory, every handoff/spec/plan "deferred / out-of-scope / next"
section, in-code markers (`TODO`/`FUTURE`/`NOTE`/`not supported`), and the frontend gaps
that are rooted in missing backend capability. Citations are file:line where available.

Context: the **frontend rebuild is complete through full admin parity** (Spec 3c done).
Everything below is backend (or backend-blocked) work. Tiers are by leverage, not effort.

**SCOPE DECISION (2026-06-08):** Federation directionality is fixed.
- **Upstream (sign-IN to Prohibitorum):** Passkey, Password, TOTP, and **OIDC** federation ONLY.
- **Downstream (Prohibitorum as IdP for relying parties):** OIDC **and** SAML.
- **SAML is never an upstream/login protocol.** The former item "E — SAML-as-login
  (upstream SAML RP)" is **OUT OF SCOPE — not deferred, will not be built.** SAML stays
  strictly downstream (we issue assertions; we never consume them). "SAML invites" are
  therefore also out of scope; invites use passkey / OIDC-federated / (future D) password-TOTP.

Legend for readiness: **[schema ready]** = tables/columns already exist; **[greenfield]** =
needs new schema + endpoints; **[code-only]** = no schema change.

---

## Tier 1 — Backend gaps that limit the EXISTING UI (highest leverage, self-contained)
These unblock features the rebuilt UI already wants; mostly small, no new protocol.

1. **`GET /me/factors` — factor-status read endpoint.** `[schema ready, code-only]`
   Security cards (`PasswordCard`/`TotpCard`/`RecoveryCodesCard`) are stateless — they can't
   show "password set", "TOTP enrolled", or "N of 10 recovery codes left". Needs a cross-table
   read (`totp_credential.confirmed_at`, `password_credential` existence,
   `COUNT(recovery_code WHERE used_at IS NULL)`) + contract type + handler.
   Src: `memory/project_deferred_security_ui_backend.md`; `specs/2026-06-07-frontend-rebuild-security-design.md` §10; FE `SecurityView.vue`.

2. **`PUT /me` — self-service profile update (displayName).** `[code-only]`
   `ProfileView` is entirely read-only; a user cannot change their own display name (only an
   admin can, via `PUT /accounts/:id`). Username stays immutable (`username_immutable`).
   Src: FE `ProfileView.vue:2`, `locales/en.ts:462`.

3. **Granular factor disable — `POST /me/totp/delete`, `POST /me/password/delete`.** `[code-only]`
   Only the coarse `POST /me/auth/revoke-password-totp` exists (wipes password+TOTP+recovery
   together). No way to drop just one factor. Needs a factor-policy decision (is password-only
   w/o TOTP allowed?).
   Src: `memory/project_deferred_security_ui_backend.md`; FE `SecurityView.vue:33`.

4. **D — Password/TOTP enrollment ceremony.** `[greenfield]`
   Enrollment is passkey-only (or federation redirect). "Invite requires password/TOTP setup"
   needs: a credential-requirements column on `enrollment`, new ceremony endpoints
   (`/enrollments/{token}/password|totp/...`), then FE forms. The user hit this directly.
   Label **D**. Src: every recent handoff §NEXT WORK; FE `EnrollView.vue:10-18`.

5. **Admin `GET /accounts/:id/sessions` — list a target account's sessions.** `[code-only]`
   Admin detail page can only bulk "revoke all sessions" + show a count; can't inspect or
   selectively revoke one session of another user.
   Src: FE `AdminAccountDetailView.vue:183-188`.

6. **Admin account attribute editing.** `[code-only]`
   `attributes` is shown read-only; `PUT /accounts/:id` full-replaces it (FE round-trips it
   verbatim). No granular add/edit/remove. Needs safe PATCH semantics or an attributes endpoint.
   Src: FE `AdminAccountDetailView.vue:4,156-160`.

7. **AAGUID → provider/authenticator name on `GET /me/credentials`.** `[needs dataset]`
   `webauthn_credential.aaguid` is stored but not resolved; unknown passkeys render as generic
   "Passkey ····{suffix}". Needs a bundled AAGUID dataset or FIDO MDS lookup + a resolved field.
   Src: `memory/project_deferred_security_ui_backend.md`; FE `PasskeysCard.vue:51`.

8. **WebAuthn Signal API support on passkey delete.** `[code-only, minimal]`
   `POST /me/credentials/delete` should return the remaining credential-id list so the FE can
   call `signalAllAcceptedCredentials()` and avoid phantom passkeys at the provider. Low browser
   support today.
   Src: `memory/project_deferred_security_ui_backend.md`; FE `PasskeysCard.vue:80-89`.

9. **SAML `PUT /saml-providers/{id}` omits `attribute_map` / `name_id_claim`.** `[code-only]`
   Admin must delete+recreate an SP to change attribute mapping. `UpdateSAMLSP` sqlc query
   excludes these fields.
   Src: `notes/2026-06-06-admin-management-api-handoff.md` §caveats.

---

## Tier 2 — Correctness / security hardening (existing features, narrow gaps)

10. **KV compare-and-swap primitive** — the structural root of three races. `[code, KV interface change]`
    `kv.Store` exposes no atomic CAS (Redis WATCH/Lua). Three non-atomic Get→Set paths depend on it:
    - OIDC **refresh-token rotation** race → legitimate-client victim-lockout (`pkg/protocol/oidc/refresh.go:156-165`).
    - SAML **AuthnRequest replay** detection (`pkg/protocol/saml/authnreq.go:347-351`).
    - OIDC **auth-code concurrent replay** mint window (`notes/2026-05-30-session-handoff.md`).
    Fix once at the interface → fixes all three.

11. **Wrap multi-credential deletes in a DB transaction.** `[code-only]`
    `revoke-password-totp` / `DisableNonWebAuthnFallbacks` delete recovery codes + TOTP + password
    as 3 unwrapped queries; partial failure leaves mixed state.
    Src: `pkg/authn/flow.go:105`, `pkg/server/handle_me_revoke_pwd_totp.go:9-11`.

12. **OIDC client-id timing oracle.** `[code-only]` Unknown `client_id` at `/oauth/token` returns
    before argon2id verify → enumeration oracle. Fix: dummy verify against a fixed PHC.
    Src: `pkg/protocol/oidc/client.go:89-94`.

13. **Account-existence privacy on `/auth/methods?username=`.** `[code + UX decision]`
    Reveals whether a username exists (NIST 800-63B-4 discourages). Needs an opaque/constant-time
    response + a UX decision.
    Src: `specs/2026-05-25-v0.2-password-totp-design.md`.

14. **Password breach-list check** at `POST /me/password/set`. `[needs dataset/service]`
    No HIBP/known-breach check. Needs a bundled dataset or k-anonymity API + block-vs-warn policy.
    Src: v0.7+ hardening bucket.

---

## Tier 3 — New major subsystems (large, demand-driven)

15. **Email channel (SMTP).** `[greenfield]`
    "No email channel; admin-issued enrollment is the only recovery path" (`ARCHITECTURE.md:17-18`).
    Blocks email invites, self-service password reset, email verification. Foundational for several
    consumer-style flows.

> **REMOVED — formerly "E — SAML-as-login (upstream SAML RP)":** out of scope per the
> 2026-06-08 scope decision above. SAML is downstream-only; there will be no upstream SAML
> consumer, no `upstream_saml` table, and no SAML invites.

---

## Tier 4 — Protocol completeness (deferred, low current demand)

17. **SAML SLO front-channel propagation.** `[schema ready: saml_session]` Logout from one SP
    doesn't log the user out of other SPs sharing the IdP session. Src: `pkg/protocol/saml/slo.go:43`.
18. **SAML assertion / NameID encryption (xmlenc).** `[schema ready: saml_sp_key use=encryption]`
    Assertions are signed, not encrypted. Src: `specs/2026-05-30-v0.5-saml-idp-design.md`.
19. **SAML AttributeQuery / NameIDMapping / Artifact binding.** `[greenfield]` Three unimplemented
    SAML profiles; none required by GHES.
20. **OIDC upstream back-channel logout reception.** `[greenfield]` No handler for an upstream IdP's
    logout notification; the federated local session stays alive. Src: `specs/2026-05-28-v0.3-*`.
21. **Upstream refresh-token storage + proactive refresh.** `[greenfield]` Upstream tokens are
    consumed then discarded; can't refresh or detect upstream revocation. Src: `specs/2026-05-28-v0.3-*`.
22. **Per-IdP AMR override** (`default_amr` column on `upstream_idp`). `[schema + code]` Src: v0.3 design.
23. **OIDC advanced extensions** — PAR (9126), JAR (9101), DPoP (9449), mTLS (8705), Dynamic Client
    Registration (7591/7592), pairwise `sub`, encrypted ID tokens (JWE); also no device-code /
    client-credentials / implicit / hybrid grants (only `authorization_code`+`refresh_token`,
    `response_type=code`). `[greenfield each]` Src: `specs/2026-05-29-v0.4-*`, `token.go`/`authorize.go`.
24. **TOTP SHA-256/512 widening** (HMAC-SHA1 only today). `[code-only]` Src: `pkg/credential/totp/code.go:7-11`.
25. **Passkey-based account recovery** (recovery session has no `phase`; TOTP-recovery only).
    `[code + schema]` Src: `pkg/server/handle_auth_recovery.go:57-60`.
26. **Emergency force-retire of a signing key** (straight to `retired`, bypassing the grace window)
    for compromise response. `[code-only]` Src: `specs/2026-06-06-admin-management-api-design.md` §6.3.

---

## Tier 5 — Operational / infra / data

27. **Migration 009 — drop legacy `signing_key.active` / `retired_at`.** `[migration]` `008` expanded +
    dual-writes; `009` (contract) not written yet — run after a production soak. Src: `ARCHITECTURE.md:276,374`.
28. **Multi-replica cache coherence.** `[infra/code]` Signing-key cache + rate limiter are per-process
    (5-min TTL; local `InvalidateKeyCache` only). Needs pub/sub or LISTEN/NOTIFY. Related to #10's KV work.
    Src: `notes/2026-06-06-admin-management-api-handoff.md` §caveats.
29. **DEK batch re-encryption tool.** `[code-only]` TOTP secrets + `upstream_idp.client_secret_enc` are
    re-encrypted lazily on touch; no proactive batch tool for a deliberate DEK rotation. Src: v0.2/v0.3 designs.
30. **Refresh-token family forensics table (Postgres).** `[schema + code]` Families live in KV only;
    a PG table would add durable audit beyond TTL. Src: `specs/2026-05-24-*`, `2026-05-29-*`.
31. **Audit-log SIEM export** (webhook/syslog push). `[greenfield]` `GET /audit-events` is pull-only.
32. **HSM/KMS for signing keys.** `[infra]` `signing_key.private_pem` is plaintext in Postgres; no
    envelope encryption/KMS. Also: extend data-at-rest encryption beyond TOTP secrets (`configx.go:39-40`).
33. **Upstream IdP create atomicity** — insert-then-seal has a crash gap (fails closed → unusable IdP).
    Make it one transaction / add startup compensation. Src: `api.md` §Upstream IdPs.
34. **Admin settings page backend.** `[greenfield + scope decision]` `/admin/settings` is a FE placeholder;
    no runtime-settings API exists; needs a decision on runtime-config vs deploy-time env.
35. **Playwright e2e** — only vitest + `cmd/smoke` (API-level) exist; no real-browser ceremony/redirect coverage.

---

## Code hygiene (minor; not functionality)
- `parseRSAPrivatePEM` duplicated across OIDC and SAML packages (`pkg/protocol/saml/keys_saml.go:141-142`) — deliberate no-cross-coupling, but two copies drift.
- `pkg/federation/oidc/modes.go:49` godoc still calls `invite_only` a "stub" though it's fully implemented — stale comment.
- FE follow-ups (out of backend scope): `AdminOidcClientDetailView` not-found double-render (same `&& !notFound` fix as the upstream view); `lines()` helper duplicated across admin views; SAML manual-create zero-ACS friendlier hint; zh-CN i18n pass (`TODO(i18n)`).

---

## Suggested sequencing (my recommendation)
- **Quick wins first (Tier 1, mostly code-only):** #1 `GET /me/factors`, #2 `PUT /me`, #3 granular factor disable, #5 admin sessions list — these visibly complete the existing UI for low cost.
- **Then D (#4)** — the enrollment-ceremony gap the user just hit; medium, needs a migration.
- **Then the hardening cluster #10 (KV CAS)** — one interface change retires three documented races + relates to #28.
- **Email (#15)** is its own cycle, demand-driven. (SAML-as-login is out of scope — see scope decision.)
- **Tier 4/5** as specific deployments require them; **#27 (009)** after a soak.

Each item above should still go through brainstorm → spec → plan before building.
