import createClient from "openapi-fetch";
import { ApiError, isCancellation, isPublicError } from "@/api/errors";
import type { paths } from "@/api/generated/schema";
import type { RawPaths } from "@/api/raw-paths";

export const client = createClient<paths & RawPaths>({
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
