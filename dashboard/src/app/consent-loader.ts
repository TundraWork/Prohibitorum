import { redirect } from "@tanstack/react-router";
import { ApiError } from "@/api/errors";
import { sessionQueryOptions } from "@/api/queries";
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
 */
export async function loadConsentRequest<T>(
  { queryClient }: RouterContext,
  href: string,
  ticket: string | undefined,
  load: (ticket: string) => Promise<T>,
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
    throw error;
  }
}
