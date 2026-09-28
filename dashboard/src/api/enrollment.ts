import { ApiError } from "@/api/errors";
import type { components } from "@/api/generated/schema";

type FederationProvider = components["schemas"]["FederationProvider"];

export type EnrollmentIntent =
  | "bootstrap"
  | "invite"
  | "federated_register"
  | "reset";

export type EnrollmentMethod = "passkey" | "password_totp";

/** `GET /enrollments/{token}`, as the page reads it. */
export interface EnrollmentPreview {
  intent: EnrollmentIntent;
  /** The username the link fixes, when the reader does not choose one. */
  username?: string;
  /** The account a reset sets up a new sign-in for; always there for a reset. */
  target?: { username: string; displayName: string };
  expiresAt: string;
  suggestedDisplayName?: string;
  allowedMethods: EnrollmentMethod[];
  /** An invitation that must be accepted through this provider. */
  expectedUpstreamIdpSlug?: string;
  providers: FederationProvider[];
}

const intents: readonly EnrollmentIntent[] = [
  "bootstrap",
  "invite",
  "federated_register",
  "reset",
];

const methods: readonly EnrollmentMethod[] = ["passkey", "password_totp"];

function isOneOf<T extends string>(
  value: unknown,
  allowed: readonly T[],
): value is T {
  return (
    typeof value === "string" && (allowed as readonly string[]).includes(value)
  );
}

/**
 * Narrows the preview the server sends, whose schema types `intent` as any
 * string and `allowedMethods` as possibly `null`. A value the page does not
 * know how to draw is a response it cannot use, so it fails as one rather than
 * guessing: an unknown intent or method, no method at all, a reset without the
 * account it resets, or an invitation bound to a provider the list lacks.
 */
export function readEnrollmentPreview(
  raw: components["schemas"]["EnrollmentPreview"],
): EnrollmentPreview {
  const invalid = () => new ApiError({ kind: "invalid-response" });
  if (!isOneOf(raw.intent, intents)) throw invalid();
  const allowed = raw.allowedMethods ?? [];
  if (allowed.length === 0) throw invalid();
  const allowedMethods: EnrollmentMethod[] = [];
  for (const method of allowed) {
    if (!isOneOf(method, methods)) throw invalid();
    allowedMethods.push(method);
  }
  if (raw.intent === "reset" && raw.target === undefined) throw invalid();
  // The server leaves the key out when there are no providers to offer.
  const providers = raw.providers ?? [];
  const bound = raw.expectedUpstreamIdpSlug;
  if (
    bound !== undefined &&
    !providers.some((provider) => provider.slug === bound)
  ) {
    throw invalid();
  }
  return {
    intent: raw.intent,
    ...(raw.username ? { username: raw.username } : {}),
    ...(raw.target ? { target: raw.target } : {}),
    expiresAt: raw.expiresAt,
    ...(raw.suggestedDisplayName
      ? { suggestedDisplayName: raw.suggestedDisplayName }
      : {}),
    allowedMethods,
    ...(bound ? { expectedUpstreamIdpSlug: bound } : {}),
    providers,
  };
}
