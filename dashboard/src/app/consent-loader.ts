import type {
  FetchQueryOptions,
  QueryClient,
  QueryKey,
} from "@tanstack/react-query";
import { redirect } from "@tanstack/react-router";
import { ApiError } from "@/api/errors";
import { sessionQueryOptions } from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { ConsentExpiredError } from "@/pages/public/consent/ConsentExpired";
import type { RouterContext, StartedReads } from "@/routes/__root";

/** The read of the request a consent page's ticket names. */
type RequestRead<T, K extends QueryKey> = (
  ticket: string,
) => FetchQueryOptions<T, Error, T, K>;

/** A consent page's two reads, settled: the session and the request. */
type ConsentReads<T> = Promise<
  [PromiseSettledResult<unknown>, PromiseSettledResult<T>]
>;

/**
 * Starts a consent page's two reads together: the session and the request the
 * ticket names. The root route calls it as the navigation begins
 * (`startReads`), so both run beside `/config` instead of after it, and
 * `loadConsentRequest` takes the reads it started. The request's failure is
 * left to the page rather than a toast; see `loadConsentRequest`.
 */
export function startConsentReads<T, K extends QueryKey>(
  queryClient: QueryClient,
  ticket: string | undefined,
  read: RequestRead<T, K>,
): ConsentReads<T> | undefined {
  if (ticket === undefined || ticket === "") return undefined;
  return Promise.allSettled([
    queryClient.query(sessionQueryOptions()),
    queryClient.query(quietQuery(read(ticket))),
  ]);
}

/**
 * Loads the request a consent page asks about. The ticket has to be there, and
 * the account it was issued to has to be signed in: without a session the
 * page sends the reader to sign in and come back to this very address.
 *
 * The session and the request are read at once, started by the root route
 * when it can (`routeId` names the page's reads among those it started). A
 * reader without a session is sent to sign in whatever the request answered,
 * and the request's own failures are drawn by the page — the sign-in, the
 * expired card or the error view — so none of them is also a toast.
 *
 * `retryTo` is the OIDC authorization address the consent page was sent from,
 * when there is one. An unusable ticket then turns into a `ConsentExpiredError`
 * carrying it — the one way a consumed or expired ticket comes back is to
 * start the authorization again — and the card the error draws decides
 * whether that address is one this site can offer. The SAML page passes
 * nothing and its failures stand as they are.
 */
export async function loadConsentRequest<T, K extends QueryKey>(
  {
    queryClient,
    startedReads,
  }: RouterContext & { startedReads?: StartedReads },
  routeId: string,
  href: string,
  ticket: string | undefined,
  read: RequestRead<T, K>,
  retryTo?: string,
): Promise<T> {
  const started = startedReads?.get(routeId) as ConsentReads<T> | undefined;
  // A read started for this navigation is used once; a reload asks again.
  startedReads?.delete(routeId);
  const reads = started ?? startConsentReads(queryClient, ticket, read);
  if (reads === undefined) {
    throw new ApiError({ kind: "local", code: "invalid_consent_ticket" });
  }
  const signIn = redirect({ to: "/login", search: { return_to: href } });
  const [session, request] = await reads;
  if (session.status === "rejected") throw session.reason;
  if (session.value === null) throw signIn;
  if (request.status === "fulfilled") return request.value;
  const error: unknown = request.reason;
  if (error instanceof ApiError && error.code === "no_session") throw signIn;
  if (
    retryTo !== undefined &&
    error instanceof ApiError &&
    error.code === "invalid_consent_ticket"
  ) {
    throw new ConsentExpiredError(retryTo);
  }
  throw error;
}
