import createClient from "openapi-fetch";
import { ApiError, isCancellation } from "@/api/errors";
import { httpFailure, networkFailure } from "@/api/exchange";
import type { paths } from "@/api/generated/schema";
import type { RawAdminPaths } from "@/api/raw-admin-paths";
import type { RawPaths } from "@/api/raw-paths";
import {
  accountHeader,
  headerAccountId,
  requestTabAccountCheck,
  tabAccountAtom,
} from "@/app/tab-account";
import { store } from "@/i18n";

/** Paths the generated schema documents as reads that also take hand-written writes. */
type ExtendedPathKey =
  | "/api/prohibitorum/identity-providers"
  | "/api/prohibitorum/identity-providers/{slug}"
  | "/api/prohibitorum/oidc-applications"
  | "/api/prohibitorum/oidc-applications/{clientId}"
  | "/api/prohibitorum/forward-auth-apps"
  | "/api/prohibitorum/forward-auth-apps/{clientId}"
  | "/api/prohibitorum/saml-applications"
  | "/api/prohibitorum/saml-applications/{id}";

/** The self-service counterpart: Huma documents the read, the writes are raw HTTP. */
type ExtendedSelfPathKey = "/api/prohibitorum/me/avatar";

/**
 * Folds the hand-written methods into their generated path entries.
 *
 * Intersecting the two whole path objects does not work: both sides name the
 * same method keys, and the generated entry spells the ones it does not document
 * as `post?: never`, so `never` wins and the write disappears. Dropping exactly
 * the keys the hand-written side supplies — and only those — keeps the generated
 * reads, including their path-level `parameters`, and lets the writes through.
 */
type WithExtraMethods<Base, Extra> = {
  [K in keyof Base]: K extends keyof Extra
    ? Omit<Base[K], keyof Extra[K]> & Extra[K]
    : Base[K];
};

/**
 * The hand-written paths for operations Huma does not document at all — the
 * raw-HTTP admin mutations, login and logout, and the federation and application
 * writes — layered over the generated schema. A path that does not appear in
 * `ExtendedPathKey` or `ExtendedSelfPathKey` is only ever declared on one side,
 * so a plain union is enough; those are declared on both and go through
 * `WithExtraMethods`.
 *
 * The generated side also used to be dropped for `/invitations`, `/audit-events`
 * and `/signing-keys`, because Huma left the shared `PageInput` out of the schema
 * and the generated type had no `cursor` or `limit` to page by. `PageInput` is
 * exported now, those parameters are documented, and the three hand-written GET
 * declarations have been deleted.
 */
type AdminPaths = Omit<RawPaths, ExtendedSelfPathKey> &
  Omit<RawAdminPaths, ExtendedPathKey> &
  WithExtraMethods<
    paths,
    Pick<RawAdminPaths, ExtendedPathKey> & Pick<RawPaths, ExtendedSelfPathKey>
  >;

export const client = createClient<AdminPaths>({
  baseUrl: window.location.origin,
  credentials: "same-origin",
  // A clone goes out, so the middleware's request keeps its body for the
  // details of a failure.
  fetch: (request) => globalThis.fetch(request.clone()),
});

const apiPrefix = "/api/prohibitorum/";

// Reads and writes about the browser rather than one of its accounts: they
// name no account, and what they answer says nothing about the tab's.
const browserPaths = new Set([
  "/api/prohibitorum/auth/accounts",
  "/api/prohibitorum/auth/logout-all",
  "/api/prohibitorum/account-chooser",
]);

function accountPath(request: Request): string | undefined {
  const url = new URL(request.url);
  if (url.origin !== window.location.origin) return undefined;
  if (!url.pathname.startsWith(apiPrefix)) return undefined;
  if (browserPaths.has(url.pathname)) return undefined;
  return url.pathname;
}

/**
 * Whether a response says the tab's account may have gone: the request went
 * out as the tab's account (or, while it is the browser's only one, as the
 * default) and came back acting as another. Sign-in and sign-out answer for
 * the account they change, so they are left out, as is a request that names
 * another account on purpose, such as signing that one out.
 */
function questionsTabAccount(request: Request, response: Response): boolean {
  const pinned = store.get(tabAccountAtom);
  if (pinned === null) return false;
  const path = accountPath(request);
  if (path === undefined || path.startsWith(`${apiPrefix}auth/`)) return false;
  const tab = String(pinned.accountId);
  const sent = request.headers.get(accountHeader);
  if (sent !== null && sent !== tab) return false;
  const served = response.headers.get(accountHeader);
  return served !== null && served !== tab;
}

client.use({
  onRequest({ request }) {
    if (request.headers.has(accountHeader)) return;
    if (accountPath(request) === undefined) return;
    const id = headerAccountId();
    if (id !== undefined) request.headers.set(accountHeader, String(id));
  },
  async onResponse({ request, response }) {
    if (response.ok) {
      if (questionsTabAccount(request, response)) requestTabAccountCheck();
      return;
    }
    const failure = await httpFailure(request, response);
    // The account the header named is not signed in here.
    if (
      failure.code === "account_not_signed_in" &&
      accountPath(request) !== undefined
    ) {
      requestTabAccountCheck();
    }
    throw failure;
  },
  async onError({ error, request }) {
    if (isCancellation(error)) return;
    if (request.signal.aborted) {
      return new DOMException("The request was aborted.", "AbortError");
    }
    return await networkFailure(request, error);
  },
});

export async function requireJsonData<T>(
  result: Promise<{ data?: T; response: Response }>,
): Promise<NonNullable<T>> {
  try {
    const { data, response } = await result;
    if (data === undefined || data === null) {
      throw new ApiError({ kind: "invalid-response", status: response.status });
    }
    return data;
  } catch (error) {
    if (error instanceof ApiError || isCancellation(error)) throw error;
    throw new ApiError({ kind: "invalid-response" }, { cause: error });
  }
}
