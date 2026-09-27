import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import type {
  OidcProviderConfig,
  ProviderMode,
  ProviderProtocol,
} from "@/api/raw-admin-paths";

/**
 * The words the identity-provider pages use for the server's vocabulary.
 *
 * The list, the detail page and the create page all name the same protocols,
 * provisioning modes, token auth methods, endpoints and diagnostic states, so
 * each is written once here. A choice that is picked from a list carries a
 * `description` as well: the reader is deciding what a provider does with
 * someone it has not seen, or how this instance proves itself to it, and the
 * name alone does not say that.
 */

export interface ProviderOption<T extends string> {
  value: T;
  label: MessageDescriptor;
  description: MessageDescriptor;
}

export const providerModes: readonly ProviderOption<ProviderMode>[] = [
  {
    value: "auto_provision",
    label: msg({
      id: "admin.federation.mode.auto",
      message: "Creates accounts",
    }),
    description: msg({
      id: "admin.federation.mode.auto.hint",
      message:
        "Creates an account the first time someone without one signs in.",
    }),
  },
  {
    value: "invite_only",
    label: msg({
      id: "admin.federation.mode.invite",
      message: "Invitation only",
    }),
    description: msg({
      id: "admin.federation.mode.invite.hint",
      message:
        "Only linked accounts can sign in. New accounts need an invitation.",
    }),
  },
  {
    value: "link_only",
    label: msg({
      id: "admin.federation.mode.link",
      message: "Links existing accounts",
    }),
    description: msg({
      id: "admin.federation.mode.link.hint",
      message:
        "Links to existing accounts only. Never creates one, not even from an invitation.",
    }),
  },
];

/** A mode's name, for a trigger, a cell or a read-only value. */
export function providerModeLabel(mode: ProviderMode): MessageDescriptor {
  const option = providerModes.find((candidate) => candidate.value === mode);
  return (option ?? (providerModes[0] as ProviderOption<ProviderMode>)).label;
}

export const providerProtocolOptions: readonly ProviderOption<ProviderProtocol>[] =
  [
    {
      value: "oidc",
      label: msg({ id: "admin.federation.protocol.oidc", message: "OIDC" }),
      description: msg({
        id: "admin.federation.protocol.oidc.hint",
        message:
          "Google, Microsoft Entra ID, Keycloak and other providers that support OpenID Connect.",
      }),
    },
    {
      value: "steam",
      label: msg({ id: "admin.federation.protocol.steam", message: "Steam" }),
      description: msg({
        id: "admin.federation.protocol.steam.hint",
        message: "Sign in with a Steam account. Needs a Steam Web API key.",
      }),
    },
    {
      value: "vrchat",
      label: msg({ id: "admin.federation.protocol.vrchat", message: "VRChat" }),
      description: msg({
        id: "admin.federation.protocol.vrchat.hint",
        message:
          "Links VRChat identities to existing accounts. Needs a VRChat operator account.",
      }),
    },
  ];

/** A protocol's name. */
export function providerProtocolLabel(
  protocol: ProviderProtocol,
): MessageDescriptor {
  const option = providerProtocolOptions.find(
    (candidate) => candidate.value === protocol,
  );
  return (
    option ?? (providerProtocolOptions[0] as ProviderOption<ProviderProtocol>)
  ).label;
}

export type ClientAuthMethod = OidcProviderConfig["tokenAuthMethod"];

/**
 * How this instance authenticates to the provider's token endpoint.
 *
 * The two secret-sending methods are named by their literal value, which is
 * what the provider's own console calls them; `literal` says to draw that name
 * in monospace rather than as a translated word.
 */
export const clientAuthMethods: readonly (ProviderOption<ClientAuthMethod> & {
  literal: boolean;
})[] = [
  {
    value: "discovery",
    literal: false,
    label: msg({
      id: "admin.federation.auth.discovery",
      message: "Use what discovery reports",
    }),
    description: msg({
      id: "admin.federation.auth.discovery.hint",
      message:
        "Uses what the provider publishes. Not available with manual endpoints.",
    }),
  },
  {
    value: "client_secret_basic",
    literal: true,
    label: msg({
      id: "admin.federation.auth.basic",
      message: "client_secret_basic",
    }),
    description: msg({
      id: "admin.federation.auth.basic.hint",
      message: "Sends the client secret in the Authorization header.",
    }),
  },
  {
    value: "client_secret_post",
    literal: true,
    label: msg({
      id: "admin.federation.auth.post",
      message: "client_secret_post",
    }),
    description: msg({
      id: "admin.federation.auth.post.hint",
      message: "Sends the client secret in the request body.",
    }),
  },
  {
    value: "none",
    literal: false,
    label: msg({ id: "admin.federation.auth.none", message: "Public client" }),
    description: msg({
      id: "admin.federation.auth.none.hint",
      message: "Uses no client secret; sign-in always uses PKCE.",
    }),
  },
];

export function clientAuthMethodOption(method: ClientAuthMethod) {
  return (
    clientAuthMethods.find((candidate) => candidate.value === method) ??
    (clientAuthMethods[0] as (typeof clientAuthMethods)[number])
  );
}

/** Whether a method sends a client secret, so the provider needs one set. */
export function clientAuthNeedsSecret(method: ClientAuthMethod): boolean {
  return method !== "none";
}

export type EndpointName = "authorization" | "token" | "userinfo" | "jwks";

/** The four endpoints, in the order a sign-in reaches them. */
export const endpointNames: readonly {
  name: EndpointName;
  label: MessageDescriptor;
}[] = [
  {
    name: "authorization",
    label: msg({
      id: "admin.federation.connection.authorization",
      message: "Authorization endpoint",
    }),
  },
  {
    name: "token",
    label: msg({
      id: "admin.federation.connection.token",
      message: "Token endpoint",
    }),
  },
  {
    name: "userinfo",
    label: msg({
      id: "admin.federation.connection.userinfo",
      message: "UserInfo endpoint",
    }),
  },
  {
    name: "jwks",
    label: msg({
      id: "admin.federation.connection.jwks",
      message: "JWKS endpoint",
    }),
  },
];

export function endpointLabel(name: EndpointName): MessageDescriptor {
  const entry = endpointNames.find((candidate) => candidate.name === name);
  return (entry ?? (endpointNames[0] as (typeof endpointNames)[number])).label;
}

/**
 * The effective configuration's fields, in the order the table lists them:
 * where the provider is, the endpoints a sign-in reaches in turn, then how the
 * token request is made and what it asks for. The keys are the server's.
 */
export const effectiveConfigFields: readonly {
  key: string;
  label: MessageDescriptor;
}[] = [
  {
    key: "issuer",
    label: msg({ id: "admin.federation.effective.issuer", message: "Issuer" }),
  },
  {
    key: "authorizationEndpoint",
    label: msg({
      id: "admin.federation.effective.authorization",
      message: "Authorization",
    }),
  },
  {
    key: "tokenEndpoint",
    label: msg({ id: "admin.federation.effective.token", message: "Token" }),
  },
  {
    key: "userinfoEndpoint",
    label: msg({
      id: "admin.federation.effective.userinfo",
      message: "UserInfo",
    }),
  },
  {
    key: "jwksEndpoint",
    label: msg({ id: "admin.federation.effective.jwks", message: "JWKS" }),
  },
  {
    key: "tokenAuthMethod",
    label: msg({
      id: "admin.federation.effective.auth-method",
      message: "Client authentication",
    }),
  },
  {
    key: "pkceMethod",
    label: msg({ id: "admin.federation.effective.pkce", message: "PKCE" }),
  },
  {
    key: "scopes",
    label: msg({ id: "admin.federation.effective.scopes", message: "Scopes" }),
  },
];

/** Where a resolved value came from; unknown sources read as absent. */
export function configSourceLabel(
  source: string,
): MessageDescriptor | undefined {
  switch (source) {
    case "discovery":
      return msg({
        id: "admin.federation.diagnostics.source.discovery",
        message: "Discovered",
      });
    case "override":
      return msg({
        id: "admin.federation.diagnostics.source.override",
        message: "Overridden",
      });
    case "manual":
      return msg({
        id: "admin.federation.diagnostics.source.manual",
        message: "Manual",
      });
    default:
      return undefined;
  }
}

export type RunState = "waiting" | "checking" | "succeeded" | "failed";

/**
 * A run's status, folded into the four states the reader tells apart. `ready`
 * and `running` are both the server working through the stages after the
 * callback; the reader only needs to know it is still checking.
 */
export function runState(status: string): RunState | undefined {
  switch (status) {
    case "awaiting_callback":
      return "waiting";
    case "ready":
    case "running":
      return "checking";
    case "succeeded":
      return "succeeded";
    case "failed":
      return "failed";
    default:
      return undefined;
  }
}

export function runStatusLabel(state: RunState): MessageDescriptor {
  switch (state) {
    case "waiting":
      return msg({
        id: "admin.federation.diagnostics.run.waiting",
        message: "Waiting for the provider",
      });
    case "checking":
      return msg({
        id: "admin.federation.diagnostics.run.checking",
        message: "Checking",
      });
    case "succeeded":
      return msg({
        id: "admin.federation.diagnostics.run.succeeded",
        message: "Test passed",
      });
    case "failed":
      return msg({
        id: "admin.federation.diagnostics.run.failed",
        message: "Test failed",
      });
  }
}

/** The server's stage names, as the reader knows them. */
export function stageName(name: string): MessageDescriptor | undefined {
  switch (name) {
    case "discovery":
      return msg({
        id: "admin.federation.diagnostics.stage.discovery",
        message: "Discovery",
      });
    case "authorize":
      return msg({
        id: "admin.federation.diagnostics.stage.authorize",
        message: "Authorization",
      });
    case "callback":
      return msg({
        id: "admin.federation.diagnostics.stage.callback",
        message: "Callback",
      });
    case "token_exchange":
      return msg({
        id: "admin.federation.diagnostics.stage.token",
        message: "Token exchange",
      });
    case "id_token":
      return msg({
        id: "admin.federation.diagnostics.stage.id-token",
        message: "ID token",
      });
    case "userinfo":
      return msg({
        id: "admin.federation.diagnostics.stage.userinfo",
        message: "UserInfo",
      });
    default:
      return undefined;
  }
}

export type StageState = "succeeded" | "failed" | "skipped" | "pending";

export function stageState(status: string): StageState {
  return status === "succeeded" || status === "failed" || status === "skipped"
    ? status
    : "pending";
}

export function stageStatusLabel(state: StageState): MessageDescriptor {
  switch (state) {
    case "succeeded":
      return msg({
        id: "admin.federation.diagnostics.stage-status.succeeded",
        message: "Passed",
      });
    case "failed":
      return msg({
        id: "admin.federation.diagnostics.stage-status.failed",
        message: "Failed",
      });
    case "skipped":
      return msg({
        id: "admin.federation.diagnostics.stage-status.skipped",
        message: "Skipped",
      });
    case "pending":
      return msg({
        id: "admin.federation.diagnostics.stage-status.pending",
        message: "Pending",
      });
  }
}
