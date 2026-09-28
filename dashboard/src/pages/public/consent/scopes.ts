import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import {
  IdCard,
  KeyRound,
  type LucideIcon,
  Mail,
  RefreshCw,
  UserRound,
  UsersRound,
} from "lucide-react";

/**
 * What an OIDC scope lets an application do, as the consent page lists it.
 * The server names the scopes and leaves the wording to the console.
 * `{instance}` in a description is the instance's name.
 */
export interface ScopeDescription {
  icon: LucideIcon;
  /** The scope's name; `undefined` for one the app defined, shown as written. */
  name?: MessageDescriptor;
  description: MessageDescriptor;
}

const knownScopes: Readonly<Record<string, ScopeDescription>> = {
  openid: {
    icon: IdCard,
    name: msg({ id: "consent.scope.openid", message: "Confirm who you are" }),
    description: msg({
      id: "consent.scope.openid.description",
      message: "Know which {instance} account is yours",
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
      message: "Your email address and whether it is verified",
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
      message: "Keep access while you're away, until you revoke it",
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

export function describeScope(scope: string): ScopeDescription {
  return (
    (Object.hasOwn(knownScopes, scope) && knownScopes[scope]) || customScope
  );
}
