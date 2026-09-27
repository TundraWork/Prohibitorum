import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import { reservedAliasNames } from "@/api/federation";

/**
 * The rules for the values an OIDC application carries that the server does
 * not check, or checks only after the fact.
 *
 * The create page and the detail page edit the same redirect addresses and the
 * same scopes, so both read the rules from here: a rule written twice is a rule
 * that ends up disagreeing with itself. None of them repairs a value. A space
 * pasted at the end of an address or a claim name is refused rather than
 * trimmed, because saving something other than what was typed turns a typo now
 * into a failed sign-in later.
 */

/** A scope the instance knows, in the order discovery lists them. */
export type OidcScope =
  | "openid"
  | "profile"
  | "email"
  | "offline_access"
  | "groups";

/**
 * The server's closed scope vocabulary (`pkg/protocol/oidc/scopes.go`), each
 * with a line saying what granting it gives the client. The line describes what
 * the instance actually releases for it (`claims.go`, `token.go`), not what the
 * specification says a scope may carry.
 */
export const oidcScopes: readonly {
  value: OidcScope;
  description: MessageDescriptor;
}[] = [
  {
    value: "openid",
    description: msg({
      id: "admin.oidc-apps.scope.openid",
      message: "Required to sign in. Always included.",
    }),
  },
  {
    value: "profile",
    description: msg({
      id: "admin.oidc-apps.scope.profile",
      message: "Name, username and picture.",
    }),
  },
  {
    value: "email",
    description: msg({
      id: "admin.oidc-apps.scope.email",
      message: "Email address and whether it is verified.",
    }),
  },
  {
    value: "offline_access",
    description: msg({
      id: "admin.oidc-apps.scope.offline-access",
      message: "A refresh token, so the client stays signed in.",
    }),
  },
  {
    value: "groups",
    description: msg({
      id: "admin.oidc-apps.scope.groups",
      message: "User groups this application exposes.",
    }),
  },
];

/**
 * The saved scopes as the scope checkboxes hold them: only the ones the
 * instance knows, always with `openid`, in the vocabulary's order.
 *
 * `/authorize` refuses a request without `openid`, so a record saved without
 * it is a client that cannot sign in; saving the form once puts it back.
 */
export function scopeFormValue(saved: readonly string[]): OidcScope[] {
  const held = new Set(saved);
  return oidcScopes
    .map((scope) => scope.value)
    .filter((scope) => scope === "openid" || held.has(scope));
}

/**
 * The saved scopes a new set no longer has, in their saved order.
 *
 * Removing an allowed scope is the scope change with a consequence past the
 * save: `/authorize` refuses a request for a scope the client is not allowed,
 * so a client that still asks for it stops signing in, while refresh tokens
 * already issued keep the scope they were granted. A saved scope the checkboxes
 * do not draw, because the instance no longer knows it, is counted here too:
 * the save drops it, and the confirmation is the only place it is named.
 */
export function removedScopes(
  saved: readonly string[],
  next: readonly string[],
): string[] {
  const kept = new Set(next);
  return saved.filter((scope) => !kept.has(scope));
}

export const addressInvalid = msg({
  id: "admin.oidc-apps.field.redirect.invalid",
  message: "Use an absolute http or https address.",
});

const addressRequired = msg({
  id: "admin.oidc-apps.uri.required",
  message: "Enter an address, or remove the row.",
});

const addressSpaces = msg({
  id: "admin.oidc-apps.uri.spaces",
  message: "Remove the spaces at either end.",
});

const addressDuplicate = msg({
  id: "admin.oidc-apps.uri.duplicate",
  message: "This address is already in the list.",
});

/**
 * Why one address is not usable, or undefined when it is.
 *
 * The server stores redirect and launch addresses as given, so this is the
 * only check they get before a client's sign-in breaks. The rule is "absolute
 * http(s), with a host, without credentials" and no tighter: a native client
 * legitimately loops back to `http://127.0.0.1`, so requiring https would
 * refuse a working client.
 */
export function absoluteHttpUrlProblem(
  value: string,
): MessageDescriptor | undefined {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return addressInvalid;
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    return addressInvalid;
  }
  if (url.host === "" || url.username !== "" || url.password !== "") {
    return addressInvalid;
  }
  return undefined;
}

/** Where in a list the first problem is, and what it is. */
export interface ListProblem {
  index: number;
  message: MessageDescriptor;
}

/**
 * The first row of an address list that is not usable, or undefined when every
 * row is.
 *
 * The row is the position: an address has no identity until it is saved, so
 * the editor marks the row at fault and the message says only what is wrong
 * with it.
 */
export function uriListProblem(
  rows: readonly string[],
): ListProblem | undefined {
  const seen = new Set<string>();
  for (const [index, row] of rows.entries()) {
    if (row.trim() === "") return { index, message: addressRequired };
    if (row !== row.trim()) return { index, message: addressSpaces };
    const problem = absoluteHttpUrlProblem(row);
    if (problem !== undefined) return { index, message: problem };
    if (seen.has(row)) return { index, message: addressDuplicate };
    seen.add(row);
  }
  return undefined;
}

/** One claim the application publishes under a second name. */
export interface AliasRow {
  name: string;
  source: string;
}

/**
 * An alias's output name, as the server validates it: an identifier-shaped
 * claim name, up to 64 characters.
 */
const aliasNamePattern = /^[A-Za-z_][A-Za-z0-9_]{0,63}$/;

const aliasNameRequired = msg({
  id: "admin.oidc-apps.alias.name.required",
  message: "Enter the claim name the client will read.",
});

const aliasNameInvalid = msg({
  id: "admin.oidc-apps.alias.name.invalid",
  message:
    "Start with a letter or an underscore, then letters, digits or underscores, up to 64 characters.",
});

const aliasNameReserved = msg({
  id: "admin.oidc-apps.alias.name.reserved",
  message: "The ID token already defines this claim.",
});

const aliasNameDuplicate = msg({
  id: "admin.oidc-apps.alias.name.duplicate",
  message: "Another alias already uses this name.",
});

/** An alias table's first problem; only the name can be wrong. */
export interface AliasListProblem extends ListProblem {
  field: "name";
}

/**
 * The first alias that is not usable, or undefined when every one is.
 *
 * The name is checked as typed. The pattern already refuses a space, so a name
 * pasted with one at either end is reported as the wrong shape rather than
 * quietly saved without it.
 */
export function aliasListProblem(
  rows: readonly AliasRow[],
): AliasListProblem | undefined {
  const seen = new Set<string>();
  for (const [index, { name }] of rows.entries()) {
    const problem = aliasNameProblem(name, seen);
    if (problem !== undefined)
      return { index, field: "name", message: problem };
    seen.add(name);
  }
  return undefined;
}

function aliasNameProblem(
  name: string,
  seen: ReadonlySet<string>,
): MessageDescriptor | undefined {
  if (name === "") return aliasNameRequired;
  if (!aliasNamePattern.test(name)) return aliasNameInvalid;
  if (reservedAliasNames.includes(name)) return aliasNameReserved;
  if (seen.has(name)) return aliasNameDuplicate;
  return undefined;
}
