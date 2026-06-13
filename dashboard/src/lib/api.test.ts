import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { api } from './api'
import type { ApiError } from './api'

// Helper to build a mock Response
function mockResponse(status: number, body: string, contentType = 'application/json'): Response {
  return new Response(body, {
    status,
    headers: { 'Content-Type': contentType },
  })
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

  it('throws {code,message} on 400 with JSON error body', async () => {
    vi.mocked(fetch).mockResolvedValue(
      mockResponse(400, JSON.stringify({ code: 'invalid_request', message: 'bad param' }))
    )
    await expect(api.get('/api/test')).rejects.toMatchObject<ApiError>({
      code: 'invalid_request',
      message: 'bad param',
    })
  })

  it('throws {code:server_error,message:text} on non-JSON 500', async () => {
    vi.mocked(fetch).mockResolvedValue(
      mockResponse(500, 'Internal Server Error', 'text/plain')
    )
    await expect(api.get('/api/test')).rejects.toMatchObject<ApiError>({
      code: 'server_error',
      message: 'Internal Server Error',
    })
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
