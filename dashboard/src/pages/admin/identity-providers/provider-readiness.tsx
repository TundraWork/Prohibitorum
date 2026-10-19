import { Trans } from "@lingui/react/macro";
import type { ReactNode } from "react";
import { readOidcProviderConfig } from "@/api/federation";
import type { components } from "@/api/generated/schema";

type Provider = components["schemas"]["IdentityProviderView"];

/** What a provider that is not ready is missing, from what the page shows. */
type Cause =
  | "connection"
  | "secret"
  | "steam-key"
  | "vrchat-unset"
  | "vrchat-expired"
  | "vrchat-check";

/**
 * The thing standing between a provider and working, read from the record
 * itself so every place that says it agrees with the sections below.
 *
 * A public OIDC client holds no secret by design, so only its connection can
 * be wrong; a confidential one is told about a secret only when none is stored.
 * A VRChat provider reads its operator session, so a reason never contradicts
 * the session badge.
 */
function notReadyCause(provider: Provider): Cause {
  switch (provider.protocol) {
    case "steam":
      return "steam-key";
    case "vrchat":
      if (provider.secretStatus === "invalid") return "vrchat-expired";
      if (provider.secretStatus === "valid") return "vrchat-check";
      return "vrchat-unset";
    default: {
      const config = readOidcProviderConfig(provider.config);
      if (config?.tokenAuthMethod === "none") return "connection";
      return provider.secretConfigured ? "connection" : "secret";
    }
  }
}

/** Which sentence a provider's state calls for. */
function notReadyMood(provider: Provider): "enable" | "sign-in" | "verify" {
  if (provider.disabled) return "enable";
  return provider.allowSignIn ? "sign-in" : "verify";
}

/**
 * What a provider that is not ready still needs, in words.
 *
 * Said for the moment it is asked. A disabled provider is told what to do
 * before it can be enabled. An enabled one is told what is failing: sign-in
 * when it backs a sign-in button, and only linking and verifying when sign-in
 * is off, so the sentence never claims a failure the provider cannot have.
 *
 * `brief` is the short form a list row carries, so a row stays one line tall;
 * the page and the danger rows say the whole sentence. Both come from one
 * cause, so a list and a page never name different problems. Each case is a
 * complete message, because a sentence stitched from fragments cannot be
 * translated.
 */
export function notReadyReason(provider: Provider, brief = false): ReactNode {
  const cause = notReadyCause(provider);
  if (brief) return notReadyNeed(cause);
  const mood = notReadyMood(provider);
  switch (cause) {
    case "connection":
      if (mood === "enable") {
        return (
          <Trans id="admin.federation.not-ready.connection.enable">
            Needs a valid connection before it can be enabled.
          </Trans>
        );
      }
      return mood === "sign-in" ? (
        <Trans id="admin.federation.not-ready.connection.sign-in">
          Sign-in through this provider fails until it has a valid connection.
        </Trans>
      ) : (
        <Trans id="admin.federation.not-ready.connection.verify">
          Linking and verifying through this provider fail until it has a valid
          connection.
        </Trans>
      );
    case "secret":
      if (mood === "enable") {
        return (
          <Trans id="admin.federation.not-ready.secret.enable">
            Needs a client secret before it can be enabled.
          </Trans>
        );
      }
      return mood === "sign-in" ? (
        <Trans id="admin.federation.not-ready.secret.sign-in">
          Sign-in through this provider fails until a client secret is set.
        </Trans>
      ) : (
        <Trans id="admin.federation.not-ready.secret.verify">
          Linking and verifying through this provider fail until a client secret
          is set.
        </Trans>
      );
    case "steam-key":
      if (mood === "enable") {
        return (
          <Trans id="admin.federation.not-ready.steam.enable">
            Set the Web API key before enabling it.
          </Trans>
        );
      }
      return mood === "sign-in" ? (
        <Trans id="admin.federation.not-ready.steam.sign-in">
          Sign-in through this provider fails until the Web API key is set.
        </Trans>
      ) : (
        <Trans id="admin.federation.not-ready.steam.verify">
          Linking and verifying through this provider fail until the Web API key
          is set.
        </Trans>
      );
    case "vrchat-unset":
      if (mood === "enable") {
        return (
          <Trans id="admin.federation.not-ready.vrchat-unset.enable">
            Sign in the operator account before enabling it.
          </Trans>
        );
      }
      return mood === "sign-in" ? (
        <Trans id="admin.federation.not-ready.vrchat-unset.sign-in">
          Sign-in through this provider fails until the operator account is
          signed in.
        </Trans>
      ) : (
        <Trans id="admin.federation.not-ready.vrchat-unset.verify">
          Linking and verifying through this provider fail until the operator
          account is signed in.
        </Trans>
      );
    case "vrchat-expired":
      if (mood === "enable") {
        return (
          <Trans id="admin.federation.not-ready.vrchat-expired.enable">
            Sign in the operator account again before enabling it.
          </Trans>
        );
      }
      return mood === "sign-in" ? (
        <Trans id="admin.federation.not-ready.vrchat-expired.sign-in">
          Sign-in through this provider fails until the operator account is
          signed in again.
        </Trans>
      ) : (
        <Trans id="admin.federation.not-ready.vrchat-expired.verify">
          Linking and verifying through this provider fail until the operator
          account is signed in again.
        </Trans>
      );
    case "vrchat-check":
      if (mood === "enable") {
        return (
          <Trans id="admin.federation.not-ready.vrchat-check.enable">
            Check the operator account before enabling it.
          </Trans>
        );
      }
      return mood === "sign-in" ? (
        <Trans id="admin.federation.not-ready.vrchat-check.sign-in">
          Sign-in through this provider fails until the operator account has
          been checked.
        </Trans>
      ) : (
        <Trans id="admin.federation.not-ready.vrchat-check.verify">
          Linking and verifying through this provider fail until the operator
          account has been checked.
        </Trans>
      );
  }
}

function notReadyNeed(cause: Cause): ReactNode {
  switch (cause) {
    case "connection":
      return (
        <Trans id="admin.federation.not-ready.brief.connection">
          Needs a valid connection
        </Trans>
      );
    case "secret":
      return (
        <Trans id="admin.federation.not-ready.brief.secret">
          Needs a client secret
        </Trans>
      );
    case "steam-key":
      return (
        <Trans id="admin.federation.not-ready.brief.steam">
          Needs a Web API key
        </Trans>
      );
    case "vrchat-unset":
      return (
        <Trans id="admin.federation.not-ready.brief.vrchat-unset">
          Needs the operator account
        </Trans>
      );
    case "vrchat-expired":
      return (
        <Trans id="admin.federation.not-ready.brief.vrchat-expired">
          Operator session expired
        </Trans>
      );
    case "vrchat-check":
      return (
        <Trans id="admin.federation.not-ready.brief.vrchat-check">
          Check the operator account
        </Trans>
      );
  }
}
