import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import {
  BadgeCheck,
  IdCard,
  KeyRound,
  type LucideIcon,
  Mail,
  RefreshCw,
  UserRound,
  UsersRound,
} from "lucide-react";

/**
 * What an OIDC scope lets an application do, as the consent page and the
 * connected applications list put it.
 * The server names the scopes and leaves the wording to the console.
 * `{instance}` in a description is the instance's name.
 */
export interface ScopeDescription {
  icon: LucideIcon;
  /** The scope's name; `undefined` for one the app defined, shown as written. */
  name?: MessageDescriptor;
  description: MessageDescriptor;
  /** Values for the name and description, such as `{provider}`. */
  values?: Readonly<Record<string, string>>;
}

const knownScopes: Readonly<Record<string, ScopeDescription>> = {
  openid: {
    icon: IdCard,
    name: msg({
      id: "consent.scope.openid",
      message: "Your account identifier",
    }),
    description: msg({
      id: "consent.scope.openid.description",
      message: "Used to identify your {instance} account",
    }),
  },
  profile: {
    icon: UserRound,
    name: msg({ id: "consent.scope.profile", message: "Your profile" }),
    description: msg({
      id: "consent.scope.profile.description",
      message: "Your display name, username and avatar",
    }),
  },
  email: {
    icon: Mail,
    name: msg({ id: "consent.scope.email", message: "Your email address" }),
    description: msg({
      id: "consent.scope.email.description",
      message: "Your primary email address and its verification status",
    }),
  },
  groups: {
    icon: UsersRound,
    name: msg({ id: "consent.scope.groups", message: "Your groups" }),
    description: msg({
      id: "consent.scope.groups.description",
      message: "The groups you belong to for this app",
    }),
  },
  offline_access: {
    icon: RefreshCw,
    name: msg({
      id: "consent.scope.offline_access",
      message: "Stay connected",
    }),
    description: msg({
      id: "consent.scope.offline_access.description",
      message:
        "Ongoing access until you revoke it, including while you're offline",
    }),
  },
};

const customScope: ScopeDescription = {
  icon: KeyRound,
  description: msg({
    id: "consent.scope.custom.description",
    message: "A permission defined by the app",
  }),
};

/** `identity:<slug>`: the account's linked identities at one provider. */
const identityScopePrefix = "identity:";

const identityScope = {
  icon: BadgeCheck,
  name: msg({
    id: "consent.scope.identity",
    message: "Your {provider} identity",
  }),
  description: msg({
    id: "consent.scope.identity.description",
    message: "Your {provider} ID, display name and profile link",
  }),
};

/**
 * `providers` names the provider of each `identity:<slug>` scope; a slug it
 * does not name is shown as written.
 */
export function describeScope(
  scope: string,
  providers: Readonly<Record<string, string>> = {},
): ScopeDescription {
  if (scope.startsWith(identityScopePrefix)) {
    const slug = scope.slice(identityScopePrefix.length);
    // An own-property check, so a slug such as "constructor" never reads an
    // inherited member of the object.
    const named = Object.hasOwn(providers, slug) ? providers[slug] : undefined;
    const provider = named ?? slug;
    return { ...identityScope, values: { provider } };
  }
  return (
    (Object.hasOwn(knownScopes, scope) && knownScopes[scope]) || customScope
  );
}
