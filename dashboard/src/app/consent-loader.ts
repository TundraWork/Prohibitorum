import { redirect } from "@tanstack/react-router";
import { ApiError } from "@/api/errors";
import { sessionQueryOptions } from "@/api/queries";
import { ConsentExpiredError } from "@/pages/public/consent/ConsentExpired";
import type { RouterContext } from "@/routes/__root";

/**
 * Loads the request a consent page asks about. The ticket has to be there, and
 * the account it was issued to has to be signed in: without a session the
 * page sends the reader to sign in and come back to this very address.
 *
 * The session is read first so the usual case — a session that expired while
 * the authorization waited — goes to the sign-in page without a failed request
 * and its toast on the way. A session that ends between the two reads is
 * caught from the request itself.
 *
 * `retryTo` is the OIDC authorization address the consent page was sent from,
 * when there is one. An unusable ticket turns into a `ConsentExpiredError`
 * carrying it — the one way a consumed or expired ticket comes back is to
 * start the authorization again — and the card the error draws decides
 * whether that address is one this site can offer. The SAML page passes
 * nothing and its failures stand as they are.
 */
export async function loadConsentRequest<T>(
  { queryClient }: RouterContext,
  href: string,
  ticket: string | undefined,
  load: (ticket: string) => Promise<T>,
  retryTo?: string,
): Promise<T> {
  if (ticket === undefined || ticket === "") {
    throw new ApiError({ kind: "local", code: "invalid_consent_ticket" });
  }
  const signIn = redirect({ to: "/login", search: { return_to: href } });
  const session = await queryClient.query(sessionQueryOptions());
  if (session === null) throw signIn;
  try {
    return await load(ticket);
  } catch (error) {
    if (error instanceof ApiError && error.code === "no_session") throw signIn;
    if (error instanceof ApiError && error.code === "invalid_consent_ticket") {
      throw new ConsentExpiredError(retryTo ?? "");
    }
    throw error;
  }
}
