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

function projectRetryAfter(err: ApiError, res: Response): void {
  const raw = res.headers.get('Retry-After')
  if (!raw || !/^\d+$/.test(raw)) return
  const seconds = Number(raw)
  if (Number.isSafeInteger(seconds)) err.retryAfterSeconds = seconds
}

export interface RequestOptions { signal?: AbortSignal }

async function request<T>(method: string, path: string, body?: unknown, options: RequestOptions = {}, raw = false): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined && !raw) headers['Content-Type'] = 'application/json'
  const timeout = new AbortController()
  const timeoutId = setTimeout(() => timeout.abort(), REQUEST_TIMEOUT_MS)
  const cancel = () => timeout.abort()
  options.signal?.addEventListener('abort', cancel, { once: true })
  const signal = timeout.signal
  try {
    options.signal?.throwIfAborted()
    const res = await fetch(path, {
      method, credentials: 'include',
      headers: Object.keys(headers).length ? headers : undefined,
      body: body === undefined ? undefined : raw ? body as Blob : JSON.stringify(body),
      signal,
    })
    // Keep timeout and cancellation active while the response body is read.
    const text = await res.text()
    options.signal?.throwIfAborted()
    let data: unknown
    if (text) { try { data = JSON.parse(text) } catch { /* non-JSON response */ } }
    if (!res.ok) {
      const err = parseApiError(data, res.headers.get('X-Request-ID') ?? undefined)
      projectRetryAfter(err, res)
      maybeSignalUnauthorized(res.status, err, method)
      maybeSignalMaintenance(res.status, err)
      if (res.status >= 500 && err.code !== 'maintenance_mode') signalConnectionError(err)
      throw err
    }
    return (data ?? {}) as T
  } catch (error) {
    if (options.signal?.aborted) throw new DOMException('Request cancelled', 'AbortError')
    if (error && typeof error === 'object' && 'code' in error && typeof error.code === 'string') throw error
    const err: ApiError = { code: 'network_error' }
    signalConnectionError(err)
    throw err
  } finally { clearTimeout(timeoutId); options.signal?.removeEventListener('abort', cancel) }
}

export const api = {
  get: <T>(path: string, options?: RequestOptions): Promise<T> => request<T>('GET', path, undefined, options),
  post: <T>(path: string, body?: unknown, options?: RequestOptions): Promise<T> => request<T>('POST', path, body, options),
  put: <T>(path: string, body?: unknown, options?: RequestOptions): Promise<T> => request<T>('PUT', path, body, options),
  del: <T>(path: string, options?: RequestOptions): Promise<T> => request<T>('DELETE', path, undefined, options),
  upload: <T>(path: string, body: Blob, options?: RequestOptions): Promise<T> => request<T>('PUT', path, body, options, true),
}
