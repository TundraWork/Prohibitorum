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
    throw isApiError(data) ? data : { code: 'server_error', message: text || res.statusText }
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
