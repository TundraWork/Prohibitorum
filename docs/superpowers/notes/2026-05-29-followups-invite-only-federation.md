# Follow-up: invite_only federation mode design

> Captured 2026-05-29 during v0.3 Task 4 implementation. The
> `upstream_idp.mode = 'invite_only'` value exists in the schema and is
> referenced by the v0.3 plan, but the actual user flow was never spec'd
> in the design doc — the smoke test (steps 50–61) exercises only
> `auto_provision`, `link_only`, and self-service link.
>
> Task 4 ships invite_only as a **stub** that returns `ErrInviteRequired`
> with an audit row `reason: "invite_only_not_implemented"`. The mode is
> selectable in the schema but unusable end-to-end until this follow-up.

## The core ambiguity

`enrollment.expected_upstream_idp_slug` is a *constraint* on an invite
(this invite, if used, must be redeemed via IdP X). It is **not** an
identifier of which invite the current OIDC user is redeeming.

To bind a federation redemption to a specific invite, the system needs a
*bearer capability* — the `enrollment.token` — to travel from the invite
URL through the OIDC dance back into the federation callback. The
design question is HOW.

## Options to discuss

1. **Query-param relay via /federation/login**

   Invite URL kicks off `GET /auth/federation/<slug>/login?enrollment_token=<t>`. The federation handler validates `expected_upstream_idp_slug == slug` and the enrollment is unconsumed + unexpired, stashes the token in `FedState.EnrollmentToken`, then proceeds with the normal OIDC flow. Callback's `applyInviteOnly` consumes the token via existing `ConsumeEnrollment(token)`.

   - **Pros:** Uses existing primitives. Atomic single-use already in place. No upstream OP cooperation needed.
   - **Cons:** Token in URL → server logs / referrer leakage / browser history. Mitigations: short TTL (already in place), HTTPS-only, single-use (already in place), maybe a server-issued exchange-on-first-fetch step.

2. **Cookie-relay**

   Invite URL stages the token in a short-lived signed HTTPOnly cookie scoped to `/auth/federation/*`, then 302s to `/auth/federation/<slug>/login`. Cookie carries the token through to the callback handler. Cookie cleared on consumption or expiry.

   - **Pros:** Token never appears in URL, log, or referrer. Slightly cleaner UX (clean URLs).
   - **Cons:** More moving parts. Cookie-based flow needs careful SameSite=Lax handling so the upstream → callback hop doesn't drop it.

3. **OIDC `login_hint` round-trip**

   Pass the enrollment token as `login_hint` to the upstream OP and rely on the OP to echo it back in claims. Spec-defined hook but most upstream OPs do not consistently echo `login_hint`, so non-portable.

   - Almost certainly **not** the right answer — relying on upstream cooperation for our own state.

4. **State-KV preflight**

   The invite URL exchanges the token at our backend (one-shot) for a short-lived `flow_id` that's then stashed in FedState. The `flow_id` is the actual handle that travels through `state` to the callback. Adds a roundtrip but keeps the long-lived bearer token off the upstream and off the URL.

   - **Pros:** Token never leaves our backend after first contact.
   - **Cons:** Extra hop.

## Open subquestions

- **Email matching**: should `invite_only` ALSO require `claims.email == enrollment.template_email` (a column we don't have yet)? Or is the bearer token sufficient authorization?
- **Multiple IdPs invited**: if an admin issues an invite without `expected_upstream_idp_slug` set, can the user pick any IdP? Or is the column mandatory for `invite_only` flows?
- **Existing identity collision**: if the upstream `(iss, sub)` already maps to a different local account, what happens? (Probably the same as auto_provision — reject as `username_collision` or a new `identity_already_linked` code.)
- **Failure UX**: if the user clicks an invite link but the upstream sign-in fails or is cancelled, can they retry without invalidating the invite? Currently `ConsumeEnrollment` is atomic on success — failure paths leave the invite unconsumed, which is correct, but the user needs a clean way to restart.

## Recommendation

Brainstorm option 1 (query-param relay) vs option 2 (cookie-relay) when this follow-up is picked up. Option 4 is over-engineering unless we discover a concrete log/leak risk that 1 + short-TTL doesn't address. Option 3 should be rejected outright.

## Current stub (Task 4)

```go
// pkg/federation/oidc/modes.go
func applyInviteOnly(ctx context.Context, q ModesQueries, w audit.Writer, idp *db.UpstreamIdp, claims *Tokens, iss, sub string) (int32, bool, error) {
    // TODO(invite-only-followup): the invite_only flow needs an
    // enrollment_token passthrough mechanism. See
    // docs/superpowers/notes/2026-05-29-followups-invite-only-federation.md
    // for the open design questions. Until that lands, invite_only is
    // selectable in schema but rejects every sign-in.
    _ = w.Record(ctx, audit.Record{
        Factor: audit.FactorFederationOIDC, Event: audit.EventFail,
        Detail: map[string]any{
            "idp_slug": idp.Slug,
            "reason":   "invite_only_not_implemented",
            "upstream_iss": iss,
        },
    })
    return 0, false, authn.ErrInviteRequired()
}
```

This satisfies the spec's "ErrInviteRequired() // 403 invite_required" listing without committing to a flow design we haven't validated.
