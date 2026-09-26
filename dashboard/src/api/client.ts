import createClient from "openapi-fetch";
import { ApiError, isCancellation, isPublicError } from "@/api/errors";
import type { paths } from "@/api/generated/schema";
import type { RawAdminPaths } from "@/api/raw-admin-paths";
import type { RawPaths } from "@/api/raw-paths";

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
 * `ExtendedPathKey` is only ever declared on one side, so a plain union is
 * enough; those eight are declared on both and go through `WithExtraMethods`.
 *
 * The generated side also used to be dropped for `/invitations`, `/audit-events`
 * and `/signing-keys`, because Huma left the shared `PageInput` out of the schema
 * and the generated type had no `cursor` or `limit` to page by. `PageInput` is
 * exported now, those parameters are documented, and the three hand-written GET
 * declarations have been deleted.
 */
type AdminPaths = RawPaths &
  Omit<RawAdminPaths, ExtendedPathKey> &
  WithExtraMethods<paths, Pick<RawAdminPaths, ExtendedPathKey>>;

export const client = createClient<AdminPaths>({
  baseUrl: window.location.origin,
  credentials: "same-origin",
  fetch: (request) => globalThis.fetch(request),
});

client.use({
  async onResponse({ response }) {
    if (response.ok) return;
    let body: unknown;
    try {
      body = await response.json();
    } catch (error) {
      if (isCancellation(error)) throw error;
    }
    throw new ApiError({
      kind: "http",
      status: response.status,
      ...(isPublicError(body)
        ? { code: body.code, details: body.details, requestId: body.requestId }
        : {}),
    });
  },
  onError({ error, request }) {
    if (isCancellation(error)) return;
    if (request.signal.aborted) {
      return new DOMException("The request was aborted.", "AbortError");
    }
    return new ApiError({ kind: "network" }, { cause: error });
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
