/**
 * Typed HTTP client for the Prohibitorum API.
 *
 * Design decisions vs the old dashboard:
 * - A single `request()` function handles all methods — no per-method duplication.
 * - `credentials: 'include'` is non-negotiable; enforced unconditionally.
 * - Response body is read as text first, then JSON-parsed (defensive). This
 *   means a truncated or non-JSON 5xx still produces a usable error object.
 * - Errors always conform to `ApiError` — callers get `{code, message}` regardless
 *   of what the server actually sent. Unknown error bodies get `code: 'server_error'`.
 * - No global state or interceptors; composables own the busy/error lifecycle.
 */

export interface ApiError {
  code: string
  message: string
}

function isApiError(v: unknown): v is ApiError {
  return (
    typeof v === 'object' &&
    v !== null &&
    typeof (v as Record<string, unknown>).code === 'string' &&
    typeof (v as Record<string, unknown>).message === 'string'
  )
}

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

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }

  const res = await fetch(path, {
    method,
    credentials: 'include',
    headers: Object.keys(headers).length > 0 ? headers : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })

  const text = await res.text()

  // Attempt to parse the body as JSON regardless of status.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let data: any = undefined
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      // Non-JSON body; data stays undefined
    }
  }

  if (!res.ok) {
    const err: ApiError = isApiError(data)
      ? data
      : { code: 'server_error', message: text || res.statusText }
    maybeSignalUnauthorized(res.status, err, method)
    maybeSignalMaintenance(res.status, err)
    throw err
  }

  // A 2xx with an empty body (e.g. 204 No Content from DELETE) parses to no
  // data; return {} rather than undefined so a void success is distinguishable
  // from a failure (run() returns undefined only on error). Matches upload().
  return (data ?? {}) as T
}

async function upload<T>(path: string, body: Blob): Promise<T> {
  const res = await fetch(path, { method: 'PUT', credentials: 'include', body })
  const text = await res.text()
  let data: unknown = undefined
  if (text) {
    try { data = JSON.parse(text) } catch { /* non-JSON body */ }
  }
  if (!res.ok) {
    const err: ApiError = isApiError(data) ? data : { code: 'server_error', message: text || res.statusText }
    maybeSignalUnauthorized(res.status, err, 'PUT')
    maybeSignalMaintenance(res.status, err)
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
