# Audit report — SAML IdP

Produced 2026-05-24 by a research agent dispatched against the multi-protocol
rescope spec. Authoritative specs consulted: SAML 2.0 Core / Bindings /
Metadata / Profiles (OASIS standards); GitHub Enterprise Server SAML SSO
documentation (3.10, 3.14); crewjam/saml library reference.

## Critical (blocks correctness or GHES interop)

### C1. `acs_url` must be plural with binding + index
**Spec:** SAML Metadata §2.4.4; SAML Profiles §4.1.4.1. **Design:** single `acs_url text` is an open-redirect risk and breaks the moment an SP publishes >1 ACS endpoint. **Fix:** child table `saml_sp_acs(sp_id, idx, binding, location, is_default)`.

### C2. AudienceRestriction value source — code-level
**Verdict:** OK as designed; assertion builder must always use `saml_sp.entity_id` verbatim as `<Audience>`. Unit test.

### C3. GHES requires SP-signed AuthnRequests
GHES generates a self-signed signing cert and signs every AuthnRequest. **Spec:** Metadata §2.4.1.1, §2.4.4. **Fix:** child table `saml_sp_key(sp_id, use, cert_pem, not_after, added_at)` (multi-cert for rotation); `require_signed_authn_request bool` on `saml_sp`.

### C4. NameID Format default URI
**Verdict:** Default `urn:oasis:names:tc:SAML:1.1:nameid-format:persistent` is correct per Core §8.3.7. Unit-test the 1.1 namespace URI to avoid accidentally emitting the 2.0 form.

### C5. NameID stability not persisted
**Spec:** Core §8.3.7 — persistent NameID values must be opaque and stable. **Design:** derived from `name_id_claim` at issue time → mutates when underlying attribute changes → GHES re-links account. **Fix:** new table `saml_subject_id(account_id, sp_id, name_id, name_id_format, created_at, UNIQUE(account_id, sp_id))`. Generate opaque value on first SSO; reuse forever.

### C6. InResponseTo / Destination — code-level
**Verdict:** For SP-initiated only, opaque acceptance in-process is fine. No new table.

## Recommended (cheaper now than later)

### R1. Restructure `attribute_map`
**Spec:** Core §2.7.3 — `<Attribute>` has `Name`, `NameFormat`, `FriendlyName`, ≥1 `<AttributeValue>`. GHES needs URI-NameFormat attributes and multi-valued (`emails`, `public_keys`, `gpg_keys`).
**Fix:** ordered JSONB array of objects, not `{local: name}` map.

### R2. SLO endpoint binding + multi-cert
Child table `saml_sp_slo(sp_id, binding, location, response_location)` when SLO becomes real (v0.5).

### R3. Metadata freshness
Add `metadata_xml text`, `metadata_valid_until timestamptz`, `metadata_cache_duration interval`, `metadata_fetched_at timestamptz` on `saml_sp`.

### R4. SP-declared signing requirements
Add `want_assertions_signed bool`, `authn_requests_signed bool` on `saml_sp` (echoes SP metadata `WantAssertionsSigned` / `AuthnRequestsSigned`).

### R5. AuthnContextClassRef — code-level
Hardcode `urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport` (or stronger when WebAuthn is the factor), `Comparison=exact`. No column needed.

### R6. Multi-cert publication in IdP metadata
`signing_key` already supports many rows. At metadata-render time, include every key where `retired_at IS NULL OR retired_at > now() - rotation_grace`.

## Optional / already-deferred

- **SP encryption certs** for Salesforce-class SPs: schema room is free once `saml_sp_key.use` exists.
- **AttributeQuery / NameIDMapping / Artifact binding**: out of scope.
- **IdP-initiated SSO**: out of scope; crewjam's `ServeIDPInitiated` will need `default_relay_state` per SP if enabled.
- **SessionIndex emission**: in-process, persisted via the `saml_session` stub when SLO lands.

## New tables recommended

```sql
CREATE TABLE saml_sp_acs (
  sp_id     bigint NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  idx       int NOT NULL,
  binding   text NOT NULL,
  location  text NOT NULL,
  is_default bool NOT NULL DEFAULT false,
  PRIMARY KEY (sp_id, idx)
);

CREATE TABLE saml_sp_key (
  id        bigserial PRIMARY KEY,
  sp_id     bigint NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  use       text NOT NULL CHECK (use IN ('signing','encryption')),
  cert_pem  text NOT NULL,
  not_after timestamptz,
  added_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE saml_subject_id (
  account_id     bigint NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  sp_id          bigint NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  name_id        text NOT NULL,
  name_id_format text NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (account_id, sp_id)
);

-- forward-compat stub: populated on every SAML SSO, consumed when SLO lands in v0.5
CREATE TABLE saml_session (
  id              bigserial PRIMARY KEY,
  session_id      text NOT NULL REFERENCES session(id) ON DELETE CASCADE,
  sp_id           bigint NOT NULL REFERENCES saml_sp(id),
  name_id         text NOT NULL,
  session_index   text NOT NULL,
  not_on_or_after timestamptz NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now()
);
```

## GHES-specific call-outs

1. **Always sign both Response and Assertion.** GHES validates `Destination` only when the Response is signed.
2. **`Destination` on `<Response>` MUST equal the ACS URL** — assertion-builder concern.
3. **`<SubjectConfirmationData Recipient>` MUST equal the ACS URL** — Profiles §4.1.4.2.
4. **NameID format = persistent (1.1 URI), value stable across renames/email changes** — implement C5.
5. **`administrator` attribute name is not user-configurable on GHES.** Always emit literally as `administrator` (basic NameFormat).
6. **`emails`, `public_keys`, `gpg_keys` are multi-valued.** R1's `multi: true` is required.
7. **`public_keys` uses URI NameFormat** with `Name="urn:oid:1.2.840.113549.1.1.1"`.
8. **`SessionNotOnOrAfter` on `<AuthnStatement>` is honored** — add `session_lifetime interval NULL` on `saml_sp` for per-SP override; null = IdP default.
9. **GHES SP entity_id format**: `https://HOSTNAME`; ACS: `https://HOSTNAME/saml/consume`.
10. **GHES self-signs AuthnRequests with a 10-year cert** — `require_signed_authn_request` defaults true when SP is GHES.
