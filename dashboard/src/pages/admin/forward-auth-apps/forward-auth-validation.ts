import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";

/**
 * The rules for the values a forward-auth application carries that the server
 * does not check.
 *
 * Both the create form and the detail page's general section edit the same
 * fields, so both need the same answers, and a rule written twice is a rule
 * that ends up disagreeing with itself. These are the client's only check on
 * any of them: the hostname is stored as given.
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

/** Why a hostname is not usable, or undefined when it is. */
export function hostProblem(value: string): MessageDescriptor | undefined {
  if (value === "") return hostRequired;
  if (value.length > maxHostLength) return hostTooLong;
  return hostPattern.test(value) ? undefined : hostInvalid;
}
