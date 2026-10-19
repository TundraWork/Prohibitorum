import type { components } from "@/api/generated/schema";

type SamlApp = components["schemas"]["SAMLApplicationView"];

/**
 * When the earliest of the application's signing certificates expires.
 *
 * `keys` are the certificates the service provider published in its metadata;
 * the signing ones are what the instance checks the provider's signed requests
 * against, so those are the ones whose expiry matters. An encryption
 * certificate lapsing is not the same deadline, so the filtering happens here
 * rather than at each caller.
 *
 * The earliest wins when a certificate has been rotated and the old one is
 * still published: it is the first date on which something stops working, which
 * is the deadline a reader is looking for.
 */
export function signingKeyExpiry(app: SamlApp): string | undefined {
  let earliest: string | undefined;
  for (const key of app.keys ?? []) {
    if (key.use !== "signing" || key.notAfter === undefined) continue;
    if (earliest === undefined || key.notAfter < earliest) {
      earliest = key.notAfter;
    }
  }
  return earliest;
}

export type CertificateState =
  | { kind: "none" }
  | { kind: "no-expiry" }
  /** An expiry the server sent that is not a date. */
  | { kind: "unreadable" }
  | { kind: "valid"; notAfter: string }
  | { kind: "expired"; notAfter: string };

/** What one certificate's expiry says, from the date the server sent (if any). */
export function expiryState(
  notAfter: string | undefined,
  now: Date = new Date(),
): CertificateState {
  if (notAfter === undefined) return { kind: "no-expiry" };
  const time = new Date(notAfter).getTime();
  if (Number.isNaN(time)) return { kind: "unreadable" };
  return time < now.getTime()
    ? { kind: "expired", notAfter }
    : { kind: "valid", notAfter };
}

/** What the application's signing certificate is: the header, the list and the rows all read this. */
export function certificateState(
  app: SamlApp,
  now: Date = new Date(),
): CertificateState {
  const hasSigningKey = (app.keys ?? []).some((key) => key.use === "signing");
  if (!hasSigningKey) return { kind: "none" };
  return expiryState(signingKeyExpiry(app), now);
}
