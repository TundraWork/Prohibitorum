import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";

/**
 * The rules for the three values a forward-auth application carries that the
 * server does not check.
 *
 * Both the create form and the detail page's general section edit the same
 * fields, so both need the same answers, and a rule written twice is a rule
 * that ends up disagreeing with itself. These are the client's only check on
 * any of them: the hostname is stored as given, and a scope name the server
 * only trims is one the console refuses rather than repairs.
 */

/**
 * A hostname, as Traefik would write it in a router rule.
 *
 * The server does not validate this at all, and the value becomes the app's
 * public identity — the cookie's scope, the `Host()` matcher — so a scheme or a
 * port that leaked into it would silently protect nothing. Every label is
 * lowercase and may not start or end with a hyphen, which is what a DNS name
 * looks like once the resolver has finished with it.
 */
const hostPattern =
  /^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

/** The DNS limit on a whole name, which the pattern's label limit does not imply. */
const maxHostLength = 253;

/** One scope name's characters and length. */
const scopeNamePattern = /^[a-zA-Z0-9]([a-zA-Z0-9._:-]*[a-zA-Z0-9])?$/;

const maxScopeNameLength = 64;
const maxScopeDescriptionLength = 256;

export const hostRequired = msg({
  id: "admin.forward-auth-apps.host.required",
  message: "Enter a hostname.",
});

export const hostInvalid = msg({
  id: "admin.forward-auth-apps.host.invalid",
  message:
    "Use a lowercase hostname with no scheme and no port, such as app.example.com.",
});

export const hostTooLong = msg({
  id: "admin.forward-auth-apps.host.too_long",
  message: "Use 253 characters or fewer.",
});

export const scopeNameRequired = msg({
  id: "admin.forward-auth-apps.scope.name.required",
  message: "Enter a scope name.",
});

export const scopeNameInvalid = msg({
  id: "admin.forward-auth-apps.scope.name.invalid",
  message:
    "Use letters, digits, dots, underscores, colons and hyphens, starting and ending with a letter or a digit, up to 64 characters.",
});

export const scopeNameDuplicate = msg({
  id: "admin.forward-auth-apps.scope.name.duplicate",
  message: "A scope with this name is already in the list.",
});

export const scopeDescriptionTooLong = msg({
  id: "admin.forward-auth-apps.scope.description.too_long",
  message: "Use 256 characters or fewer.",
});

/** Why a hostname is not usable, or undefined when it is. */
export function hostProblem(value: string): MessageDescriptor | undefined {
  if (value === "") return hostRequired;
  if (value.length > maxHostLength) return hostTooLong;
  return hostPattern.test(value) ? undefined : hostInvalid;
}

/**
 * Why one scope name is not usable on its own.
 *
 * The name is refused rather than trimmed: the server trims before validating,
 * so accepting " read" here would store "read" and show the reader something
 * they did not type. Leading and trailing whitespace is therefore a failure of
 * the pattern, which is exactly what the message says.
 */
export function scopeNameProblem(value: string): MessageDescriptor | undefined {
  if (value === "") return scopeNameRequired;
  if (value.length > maxScopeNameLength) return scopeNameInvalid;
  return scopeNamePattern.test(value) ? undefined : scopeNameInvalid;
}

/** Where in a vocabulary the first problem is, and what it is. */
export interface ScopeListProblem {
  index: number;
  field: "name" | "description";
  message: MessageDescriptor;
}

/**
 * Why a whole vocabulary is not usable, or undefined when it is.
 *
 * The answer names the row and the input, so the editor can mark the one box
 * at fault: a scope has no identity until it is saved, and "the third row's
 * name" is the only way to point at it.
 */
export function scopeListProblem(
  scopes: readonly { name: string; description?: string }[],
): ScopeListProblem | undefined {
  const seen = new Set<string>();
  for (const [index, scope] of scopes.entries()) {
    const problem = scopeNameProblem(scope.name);
    if (problem !== undefined)
      return { index, field: "name", message: problem };
    if (seen.has(scope.name)) {
      return { index, field: "name", message: scopeNameDuplicate };
    }
    seen.add(scope.name);
    if ((scope.description ?? "").length > maxScopeDescriptionLength) {
      return { index, field: "description", message: scopeDescriptionTooLong };
    }
  }
  return undefined;
}

/**
 * The saved scope names a new vocabulary no longer has, in their saved order.
 *
 * Removing a scope is the one vocabulary change with a consequence past the
 * save: the server checks a token's grants against the vocabulary when the
 * token is issued, not when it is used, so a token already granted the scope
 * keeps sending it until it is revoked. A rename is a removal and an addition,
 * and is reported the same way.
 */
export function removedScopeNames(
  saved: readonly { name: string }[],
  next: readonly { name: string }[],
): string[] {
  const kept = new Set(next.map((scope) => scope.name));
  return saved.map((scope) => scope.name).filter((name) => !kept.has(name));
}
