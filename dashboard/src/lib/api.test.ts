import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  api,
  registerUnauthorizedHandler,
  registerMaintenanceHandler,
  registerConnectionErrorHandler,
} from './api'
import type { ApiError } from './api'

// Helper to build a mock Response with optional X-Request-ID header
function mockResponse(
  status: number,
  body: string,
  opts: { contentType?: string; requestId?: string } = {},
): Response {
  const headers: Record<string, string> = { 'Content-Type': opts.contentType ?? 'application/json' }
  if (opts.requestId) headers['X-Request-ID'] = opts.requestId
  return new Response(body, { status, headers })
}

describe('api', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns parsed JSON on 200', async () => {
    vi.mocked(fetch).mockResolvedValue(mockResponse(200, JSON.stringify({ hello: 'world' })))
    const result = await api.get<{ hello: string }>('/api/test')
    expect(result).toEqual({ hello: 'world' })
  })

  it('returns {} (not undefined) for a 204 empty body — e.g. DELETE', async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }))
    const result = await api.del('/api/test')
    expect(result).toEqual({})
    expect(result).not.toBeUndefined()
  })

  it('sends credentials:include on every request', async () => {
    vi.mocked(fetch).mockResolvedValue(mockResponse(200, '{}'))
    await api.get('/api/test')
    const [, opts] = vi.mocked(fetch).mock.calls[0]
    expect((opts as RequestInit).credentials).toBe('include')
  })

  it('sends credentials:include on POST', async () => {
    vi.mocked(fetch).mockResolvedValue(mockResponse(200, '{}'))
    await api.post('/api/test', { foo: 'bar' })
    const [, opts] = vi.mocked(fetch).mock.calls[0]
    expect((opts as RequestInit).credentials).toBe('include')
  })

  it('throws {code} on 400 with JSON error body, strips message', async () => {
    vi.mocked(fetch).mockResolvedValue(
      mockResponse(400, JSON.stringify({ code: 'invalid_request', message: 'bad param' }))
    )
    const err = await api.get('/api/test').catch((e: unknown) => e as ApiError)
    expect(err.code).toBe('invalid_request')
    expect((err as Record<string, unknown>).message).toBeUndefined()
  })

  it('extracts requestId from the X-Request-ID response header', async () => {
    vi.mocked(fetch).mockResolvedValue(
      mockResponse(400, JSON.stringify({ code: 'bad_request' }), { requestId: 'rid-from-header' })
    )
    const err = await api.get('/api/test').catch((e: unknown) => e as ApiError)
    expect(err.requestId).toBe('rid-from-header')
  })

  it('uses requestId from the body when the header is absent', async () => {
    vi.mocked(fetch).mockResolvedValue(
      mockResponse(400, JSON.stringify({ code: 'bad_request', requestId: 'body-rid' }))
    )
    const err = await api.get('/api/test').catch((e: unknown) => e as ApiError)
    expect(err.requestId).toBe('body-rid')
  })

  it('preserves details from the server envelope', async () => {
    vi.mocked(fetch).mockResolvedValue(
      mockResponse(400, JSON.stringify({ code: 'invalid_role', details: { allowed: ['user', 'admin'] } }))
    )
    const err = await api.get('/api/test').catch((e: unknown) => e as ApiError)
    expect(err.code).toBe('invalid_role')
    expect(err.details).toEqual({ allowed: ['user', 'admin'] })
  })

  it('throws {code:server_error} (no message) on non-JSON 500', async () => {
    vi.mocked(fetch).mockResolvedValue(
      mockResponse(500, 'Internal Server Error', { contentType: 'text/plain' })
    )
    const err = await api.get('/api/test').catch((e: unknown) => e as ApiError)
    expect(err.code).toBe('server_error')
    expect((err as Record<string, unknown>).message).toBeUndefined()
  })

  it('throws {code:server_error} on empty 500 body', async () => {
    vi.mocked(fetch).mockResolvedValue(mockResponse(500, ''))
    const err = await api.get('/api/test').catch((e: unknown) => e)
    expect((err as ApiError).code).toBe('server_error')
  })

  it('sends JSON body + Content-Type on POST', async () => {
    vi.mocked(fetch).mockResolvedValue(mockResponse(200, '{}'))
    await api.post('/api/test', { foo: 1 })
    const [, opts] = vi.mocked(fetch).mock.calls[0]
    const init = opts as RequestInit
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json')
    expect(init.body).toBe(JSON.stringify({ foo: 1 }))
  })

  it('PUT sets method correctly', async () => {
    vi.mocked(fetch).mockResolvedValue(mockResponse(200, '{}'))
    await api.put('/api/test', { x: 1 })
    const [, opts] = vi.mocked(fetch).mock.calls[0]
    expect((opts as RequestInit).method).toBe('PUT')
  })
})

describe('401 no_session seam', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    registerUnauthorizedHandler(null)
  })

  it('invokes the handler with the method on 401 no_session and still rejects', async () => {
    const spy = vi.fn()
    registerUnauthorizedHandler(spy)
    vi.mocked(fetch).mockResolvedValue(mockResponse(401, JSON.stringify({ code: 'no_session' })))
    await expect(api.get('/api/x')).rejects.toMatchObject({ code: 'no_session' })
    expect(spy).toHaveBeenCalledWith({ method: 'GET' })
  })

  it('passes PUT for an upload 401 no_session', async () => {
    const spy = vi.fn()
    registerUnauthorizedHandler(spy)
    vi.mocked(fetch).mockResolvedValue(mockResponse(401, JSON.stringify({ code: 'no_session' })))
    await expect(api.upload('/api/x', new Blob(['z']))).rejects.toMatchObject({ code: 'no_session' })
    expect(spy).toHaveBeenCalledWith({ method: 'PUT' })
  })

  it('does NOT invoke the handler on a non-no_session 401', async () => {
    const spy = vi.fn()
    registerUnauthorizedHandler(spy)
    vi.mocked(fetch).mockResolvedValue(mockResponse(401, JSON.stringify({ code: 'sudo_required' })))
    await expect(api.post('/api/x')).rejects.toMatchObject({ code: 'sudo_required' })
    expect(spy).not.toHaveBeenCalled()
  })

  it('does NOT invoke the handler on 403/500', async () => {
    const spy = vi.fn()
    registerUnauthorizedHandler(spy)
    vi.mocked(fetch).mockResolvedValue(mockResponse(403, JSON.stringify({ code: 'forbidden' })))
    await expect(api.get('/api/x')).rejects.toMatchObject({ code: 'forbidden' })
    expect(spy).not.toHaveBeenCalled()
  })
})

describe('connection-error seam', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    registerConnectionErrorHandler(null)
    registerMaintenanceHandler(null)
  })

  it('fires the connection handler with network_error when fetch rejects (server unreachable)', async () => {
    const spy = vi.fn()
    registerConnectionErrorHandler(spy)
    vi.mocked(fetch).mockRejectedValue(new TypeError('Failed to fetch'))
    await expect(api.get('/api/x')).rejects.toMatchObject({ code: 'network_error' })
    expect(spy).toHaveBeenCalledWith({ code: 'network_error' })
  })

  it('fires the connection handler on a 5xx server_error', async () => {
    const spy = vi.fn()
    registerConnectionErrorHandler(spy)
    vi.mocked(fetch).mockResolvedValue(mockResponse(500, JSON.stringify({ code: 'server_error' })))
    await expect(api.get('/api/x')).rejects.toMatchObject({ code: 'server_error' })
    expect(spy).toHaveBeenCalledTimes(1)
  })

  it('does NOT fire the connection handler on a 503 maintenance_mode (maintenance handler owns it)', async () => {
    const conn = vi.fn()
    const maint = vi.fn()
    registerConnectionErrorHandler(conn)
    registerMaintenanceHandler(maint)
    vi.mocked(fetch).mockResolvedValue(mockResponse(503, JSON.stringify({ code: 'maintenance_mode' })))
    await expect(api.get('/api/x')).rejects.toMatchObject({ code: 'maintenance_mode' })
    expect(maint).toHaveBeenCalledTimes(1)
    expect(conn).not.toHaveBeenCalled()
  })

  it('does NOT fire the connection handler on a 4xx app error', async () => {
    const spy = vi.fn()
    registerConnectionErrorHandler(spy)
    vi.mocked(fetch).mockResolvedValue(mockResponse(409, JSON.stringify({ code: 'username_taken' })))
    await expect(api.post('/api/x')).rejects.toMatchObject({ code: 'username_taken' })
    expect(spy).not.toHaveBeenCalled()
  })
})
