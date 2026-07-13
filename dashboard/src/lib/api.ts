/**
 * Typed HTTP client for the Prohibitorum API.
 *
 * Design decisions vs the old dashboard:
 * - A single `request()` function handles all methods — no per-method duplication.
 * - `credentials: 'include'` is non-negotiable; enforced unconditionally.
 * - Response body is read as text first, then JSON-parsed (defensive). This
 *   means a truncated or non-JSON 5xx still produces a usable error object.
 * - Errors always conform to `ApiError` — callers get `{code, details?,
 *   requestId?}` regardless of what the server actually sent. The server NEVER
 *   sends a display message; unknown/unparseable bodies get `code: 'server_error'`
 *   with no details. The requestId is extracted from the `X-Request-ID` response
 *   header so operators can correlate failures to diagnostic records.
 * - No global state or interceptors; composables own the busy/error lifecycle.
 */

/**
 * Re-exported so callers that imported ApiError/isApiError from './api' still
 * resolve. The canonical definitions live in errors.ts.
 */
export type { ApiError } from './errors'
export { isApiError } from './errors'
import { parseApiError, type ApiError } from './errors'

const REQUEST_TIMEOUT_MS = 15000

export type UnauthorizedHandler = (ctx: { method: string }) => void
let unauthorizedHandler: UnauthorizedHandler | null = null

/**
 * Register a handler invoked when a request returns 401 with code
 * "no_session" (a fully-absent session). Wired in main.ts to redirect reads to
 * /login and surface a banner for mutations. Pass null to clear (tests).
 */
export function registerUnauthorizedHandler(fn: UnauthorizedHandler | null): void {
  unauthorizedHandler = fn
}

function maybeSignalUnauthorized(status: number, err: ApiError, method: string): void {
  if (status === 401 && err.code === 'no_session' && unauthorizedHandler) {
    unauthorizedHandler({ method })
  }
}

export type MaintenanceHandler = () => void
let maintenanceHandler: MaintenanceHandler | null = null

/**
 * Register a handler invoked when a request returns 503 with code
 * "maintenance_mode". Wired in main.ts to set the branding store flag and
 * redirect non-admins to the maintenance page. Pass null to clear (tests).
 */
export function registerMaintenanceHandler(fn: MaintenanceHandler | null): void {
  maintenanceHandler = fn
}

function maybeSignalMaintenance(status: number, err: ApiError): void {
  if (status === 503 && err.code === 'maintenance_mode' && maintenanceHandler) {
    maintenanceHandler()
  }
}

export type ConnectionErrorHandler = (err: ApiError) => void
let connectionErrorHandler: ConnectionErrorHandler | null = null

/**
 * Register a handler invoked when a request can't reach the server: a `fetch`
 * rejection (server down/unreachable), a timeout (AbortError after
 * REQUEST_TIMEOUT_MS), or a 5xx server error. Wired in main.ts to surface a
 * global toast. Pass null to clear (tests).
 */
export function registerConnectionErrorHandler(fn: ConnectionErrorHandler | null): void {
  connectionErrorHandler = fn
}

function signalConnectionError(err: ApiError): void {
  connectionErrorHandler?.(err)
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }

  const controller = new AbortController()
  const timeoutId = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)

  let res: Response
  try {
    res = await fetch(path, {
      method,
      credentials: 'include',
      headers: Object.keys(headers).length > 0 ? headers : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal: controller.signal,
    })
  } catch {
    // Network failure (server down/unreachable) or AbortError (timeout). Surface
    // a typed network_error instead of leaking an uncaught TypeError/DOMException.
    const err: ApiError = { code: 'network_error' }
    signalConnectionError(err)
    throw err
  } finally {
    clearTimeout(timeoutId)
  }

  const text = await res.text()

  // Attempt to parse the body as JSON regardless of status.
  let data: unknown = undefined
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      // Non-JSON body; data stays undefined
    }
  }

  if (!res.ok) {
    const requestId = res.headers.get('X-Request-ID') ?? undefined
    const err: ApiError = parseApiError(data, requestId)
    maybeSignalUnauthorized(res.status, err, method)
    maybeSignalMaintenance(res.status, err)
    // 5xx → global connection handler, EXCEPT maintenance (503 maintenance_mode
    // is owned by the maintenance handler, which redirects to the maintenance
    // screen — a connection toast on top of that would be wrong).
    if (res.status >= 500 && err.code !== 'maintenance_mode') signalConnectionError(err)
    throw err
  }

  // A 2xx with an empty body (e.g. 204 No Content from DELETE) parses to no
  // data; return {} rather than undefined so a void success is distinguishable
  // from a failure (run() returns undefined only on error). Matches upload().
  return (data ?? {}) as T
}

async function upload<T>(path: string, body: Blob): Promise<T> {
  const controller = new AbortController()
  const timeoutId = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)

  let res: Response
  try {
    res = await fetch(path, { method: 'PUT', credentials: 'include', body, signal: controller.signal })
  } catch {
    const err: ApiError = { code: 'network_error' }
    signalConnectionError(err)
    throw err
  } finally {
    clearTimeout(timeoutId)
  }

  const text = await res.text()
  let data: unknown = undefined
  if (text) {
    try { data = JSON.parse(text) } catch { /* non-JSON body */ }
  }
  if (!res.ok) {
    const requestId = res.headers.get('X-Request-ID') ?? undefined
    const err: ApiError = parseApiError(data, requestId)
    maybeSignalUnauthorized(res.status, err, 'PUT')
    maybeSignalMaintenance(res.status, err)
    // 5xx → global connection handler, EXCEPT maintenance (503 maintenance_mode
    // is owned by the maintenance handler, which redirects to the maintenance
    // screen — a connection toast on top of that would be wrong).
    if (res.status >= 500 && err.code !== 'maintenance_mode') signalConnectionError(err)
    throw err
  }
  return (data ?? {}) as T
}


export const api = {
  get: <T>(path: string): Promise<T> => request<T>('GET', path),
  post: <T>(path: string, body?: unknown): Promise<T> => request<T>('POST', path, body),
  put: <T>(path: string, body?: unknown): Promise<T> => request<T>('PUT', path, body),
  del: <T>(path: string): Promise<T> => request<T>('DELETE', path),
  upload: <T>(path: string, body: Blob): Promise<T> => upload<T>(path, body),
}
