import { ApiError, isCancellation, isPublicError } from "@/api/errors";

/**
 * What a failed request sent and got back, kept on its `ApiError` so the error
 * toast can show the details on request. Secrets in the request are masked
 * before they get here; the response is kept as the server sent it.
 */
export interface RequestExchange {
  method: string;
  /** The pathname and the masked query string, without the origin. */
  path: string;
  /** The masked request body as two-space-indented JSON; unset without a body. */
  requestBody?: string;
  /** Unset when no response arrived. */
  response?: {
    status: number;
    headers: [name: string, value: string][];
    /** JSON indented by two spaces, anything else as sent; unset when empty. */
    body?: string;
  };
}

/**
 * Request fields and query parameters that carry a password, a one-time code,
 * a TOTP secret, a session token or recovery codes. Matched on the whole name,
 * so `code_challenge` stays readable.
 */
const maskedKeys = new Set([
  "password",
  "current_password",
  "code",
  "totp_code",
  "secret_base32",
  "totp_secret_base32",
  "partial_session_token",
  "token",
  "recovery_codes",
]);

const masked = "••••••";

/** Builds the error for a response that came back with a failure status. */
export async function httpFailure(
  request: Request,
  response: Response,
): Promise<ApiError> {
  let text: string | undefined;
  try {
    text = await response.text();
  } catch (error) {
    if (isCancellation(error)) throw error;
  }
  let envelope: unknown;
  try {
    envelope = text ? JSON.parse(text) : undefined;
  } catch {
    // Not JSON: a proxy's error page, say. The details still show it as sent.
  }
  const body = text === undefined ? undefined : formatBody(text);
  return new ApiError({
    kind: "http",
    status: response.status,
    ...(isPublicError(envelope)
      ? {
          code: envelope.code,
          details: envelope.details,
          requestId: envelope.requestId,
        }
      : {}),
    exchange: {
      ...(await requestPart(request)),
      response: {
        status: response.status,
        headers: [...response.headers],
        ...(body === undefined ? {} : { body }),
      },
    },
  });
}

/** Builds the error for a request that never got a response. */
export async function networkFailure(
  request: Request,
  error: unknown,
): Promise<ApiError> {
  return new ApiError(
    { kind: "network", exchange: await requestPart(request) },
    { cause: error },
  );
}

async function requestPart(request: Request): Promise<RequestExchange> {
  const body = await requestBody(request);
  return {
    method: request.method,
    path: maskedPath(request.url),
    ...(body === undefined ? {} : { requestBody: body }),
  };
}

/**
 * The request body, masked and indented. A body that is not JSON is left out
 * rather than shown unmasked.
 */
async function requestBody(request: Request): Promise<string | undefined> {
  if (request.body === null) return undefined;
  try {
    const value: unknown = JSON.parse(await request.text());
    return JSON.stringify(maskValue(value), null, 2);
  } catch {
    return undefined;
  }
}

function maskValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(maskValue);
  if (typeof value !== "object" || value === null) return value;
  return Object.fromEntries(
    Object.entries(value).map(([key, entry]) => [
      key,
      maskedKeys.has(key) ? masked : maskValue(entry),
    ]),
  );
}

/**
 * The path with the values of secret query parameters masked. The rest of the
 * query is kept as it was sent, so the order and the encoding still match.
 */
function maskedPath(url: string): string {
  const { pathname, search } = new URL(url);
  if (!search) return pathname;
  const query = search
    .slice(1)
    .split("&")
    .map((pair) => {
      const separator = pair.indexOf("=");
      const name = separator === -1 ? pair : pair.slice(0, separator);
      return maskedKeys.has(decodeQueryName(name)) ? `${name}=${masked}` : pair;
    })
    .join("&");
  return `${pathname}?${query}`;
}

function decodeQueryName(name: string): string {
  try {
    return decodeURIComponent(name.replaceAll("+", " "));
  } catch {
    return name;
  }
}

function formatBody(text: string): string | undefined {
  if (text === "") return undefined;
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}
