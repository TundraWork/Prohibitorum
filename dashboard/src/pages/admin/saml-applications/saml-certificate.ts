import type { components } from "@/api/generated/schema";

type SamlApp = components["schemas"]["SAMLApplicationView"];

/**
 * When this application's signing certificate stops being valid.
 *
 * A service provider validates assertions against the instance's signing key,
 * so that is the key whose expiry breaks sign-in; an encryption key expiring
 * would not. The server returns both uses in `keys`, so the filtering happens
 * here rather than on the caller.
 *
 * The earliest expiry wins when a key has been rotated and the old one is still
 * published for verification: it is the first date on which something stops
 * working, which is the deadline a reader is looking for.
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
  | { kind: "valid"; notAfter: string }
  | { kind: "expired"; notAfter: string };

/** What the signing certificate is, as the page's header says it. */
export function certificateState(
  app: SamlApp,
  now: Date = new Date(),
): CertificateState {
  const hasSigningKey = (app.keys ?? []).some((key) => key.use === "signing");
  if (!hasSigningKey) return { kind: "none" };
  const notAfter = signingKeyExpiry(app);
  if (notAfter === undefined) return { kind: "no-expiry" };
  return new Date(notAfter).getTime() < now.getTime()
    ? { kind: "expired", notAfter }
    : { kind: "valid", notAfter };
}
