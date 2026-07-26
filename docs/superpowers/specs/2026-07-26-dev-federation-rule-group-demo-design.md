# Dev federation rule-group demo data

**Date:** 2026-07-26  
**Status:** Approved design

## Summary

The federation development harness will maintain one complete, deterministic application-policy showcase in instance A (`prohibitorum_upstream`). The showcase is attached to the existing `dev-app` OIDC client and demonstrates delegated management, manual decisions, every supported rule fact, and every rule combinator.

The data is opt-in through `dev-seed` and enabled only for the upstream side of `dev:federation`. It therefore survives `mise run dev:federation -- --fresh` without changing the normal `dev:seed` dataset or instance B.

## Goals

- Populate instance A with a complete rule-group configuration suitable for UI and CLI demonstrations.
- Cover every supported rule fact: provider, protocol, login method, and avatar source.
- Cover `all`, `any`, `not`, and nested rule conditions.
- Include delegated manager assignment and manual allow/deny decisions.
- Produce deterministic matching and non-matching preview results from existing demo accounts.
- Keep reruns idempotent and preserve unrelated local development data.

## Non-goals

- Demonstrate every application protocol. The showcase uses one OIDC application.
- Add the policy showcase to normal `dev:seed` or federation instance B.
- Create credentials that a person can use to authenticate.
- Delete or rewrite user-created groups that do not use the showcase's stable slugs.
- Turn the sample into production migration data.

## Entry point and isolation

`dev-seed` gains an explicit policy-demo option. Without the option, its behavior remains unchanged.

`scripts/dev-federation.sh` passes the option only while seeding the upstream instance. The downstream invocation remains the existing base seed. Because the harness runs `dev-seed` on every start, the upstream showcase is repaired on reuse and recreated after `--fresh`.

The option remains protected by the existing loopback-origin guard. No production startup path invokes it.

## Showcase application and accounts

The showcase targets the existing public OIDC client:

- client ID: `dev-app`
- display name: `Example App (dev)`
- access state: restricted

Existing seed accounts are reused:

| Account | Policy-demo role | Deterministic facts | Manual decision |
|---|---|---|---|
| `alice` | Assigned `app_manager` for `dev-app` | passkey, confirmed `google` OIDC connection, federation login, user-uploaded avatar | neutral |
| `bob` | user | password + confirmed TOTP, any avatar without a user-uploaded source | allow |
| `carol` | user | no matching credential, connection, or avatar facts | deny |
| `dave` | disabled user | none | none; omitted from active-account previews |

Promoting Alice to `app_manager` does not grant application usage. Her eligibility remains entirely rule-driven because she has no manual decision.

Credential fixtures use random, non-recoverable material and do not emit usable secrets. They exist only to satisfy the same database-backed fact predicates used by production policy evaluation. Existing usable local credentials are never replaced.

## Groups

All groups bind immutably to `dev-app`. Stable slugs make them idempotent and distinguish them from unrelated local groups.

### Manual group

- `demo-manual-access`: exposed downstream
- Bob: `allow`
- Carol: `deny`
- Alice: no row, therefore neutral

### Leaf rule groups

Each leaf group is exposed downstream:

| Slug | Condition |
|---|---|
| `demo-google-connected` | `connection.provider == google` |
| `demo-oidc-connected` | `connection.protocol == oidc` |
| `demo-passkey-login` | `login_method == passkey` |
| `demo-password-totp-login` | `login_method == password_totp` |
| `demo-federation-login` | `login_method == federation` |
| `demo-any-avatar` | `avatar == any` |
| `demo-user-avatar` | `avatar == user_uploaded` |

### Combinator rule groups

| Slug | Exposure | Condition and purpose |
|---|---|---|
| `demo-all-strong-profile` | exposed | `all(passkey, user_uploaded avatar)`; demonstrates conjunction |
| `demo-any-strong-login` | exposed | `any(passkey, password_totp)`; demonstrates disjunction |
| `demo-no-user-avatar` | hidden | `not(user_uploaded avatar)`; demonstrates negation without projecting a negative trait downstream |
| `demo-trusted-federated-profile` | exposed | `all(google provider, oidc protocol, any(federation login, passkey), not(password_totp))`; demonstrates nested composition |

All rule documents use version 1 and are canonicalized only after `appaccess.ParseAndValidateRule` accepts them against the database's known provider slugs.

## Expected preview matrix

`true` means the group matches the account's seeded facts. Carol remains the active negative baseline.

| Rule slug | Alice | Bob | Carol |
|---|---:|---:|---:|
| `demo-google-connected` | true | false | false |
| `demo-oidc-connected` | true | false | false |
| `demo-passkey-login` | true | false | false |
| `demo-password-totp-login` | false | true | false |
| `demo-federation-login` | true | false | false |
| `demo-any-avatar` | true | true | false |
| `demo-user-avatar` | true | false | false |
| `demo-all-strong-profile` | true | false | false |
| `demo-any-strong-login` | true | true | false |
| `demo-no-user-avatar` | false | true | true |
| `demo-trusted-federated-profile` | true | false | false |

Dave must not appear because preview queries exclude disabled accounts. Other locally created active accounts may appear with results determined by their current facts; the assertions for the named demo accounts remain deterministic.

## Idempotency and preservation

The seed performs one transaction after the base providers, accounts, and `dev-app` exist.

- Set Alice's role to `app_manager` and ensure her `dev-app` manager assignment exists.
- Ensure the named fact fixtures exist without replacing pre-existing credentials or identities.
- Set `dev-app.access_restricted = true`.
- Upsert groups by `(dev-app, slug)` and repair their names, descriptions, exposure flags, and rules.
- Ensure Bob's allow and Carol's deny decisions target the named manual group.
- Remove no unrelated manager assignments, groups, decisions, credentials, identities, or avatars.

A conflicting showcase slug with the wrong group kind is an error rather than a destructive delete-and-recreate. This protects local work while making configuration drift visible.

## Failure handling

Any missing prerequisite (`dev-app`, Alice/Bob/Carol, or Google provider), validation error, slug-kind conflict, or database failure aborts the policy-demo transaction and the `dev-seed` command. Partial showcase state is not committed.

Base `dev-seed` behavior remains independently idempotent. The policy option is applied after base provider, account, and application seeding so prerequisites are available on both fresh and reused databases.

## Verification

1. Run the upstream policy-demo seed twice against `prohibitorum_upstream`; both runs succeed and row counts remain stable.
2. Verify `dev-app` is restricted, Alice is an assigned `app_manager`, and Bob/Carol have the expected manual decisions.
3. Preview every named rule and compare Alice/Bob/Carol with the expected matrix.
4. Verify Dave is absent from previews.
5. Verify the showcase slugs and manager assignment are absent from `prohibitorum_downstream`.
6. Run the focused `dev-seed` tests covering opt-in isolation, idempotency, rule validation, and conflict rollback.
