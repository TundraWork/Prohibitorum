import { describe, it, expect } from 'vitest'
import { createQueryClient, clearSessionQueries } from './client'

describe('query lifecycle', () => {
  it('deduplicates reads, retains public state and cancels private work before clearing', async () => {
    const client = createQueryClient()
    let calls = 0
    let finish!: (value: string) => void
    let signal!: AbortSignal
    const options = { queryKey: ['session', 'accounts'], queryFn: (ctx: { signal: AbortSignal }) => { calls++; signal = ctx.signal; return new Promise<string>(resolve => { finish = resolve }) } }
    const first = client.fetchQuery(options).catch(() => null)
    const second = client.fetchQuery(options).catch(() => null)
    client.setQueryData(['public', 'config'], { instanceName: 'Test' })
    expect(calls).toBe(1)
    await clearSessionQueries(client)
    expect(signal.aborted).toBe(true)
    finish('old account')
    await Promise.all([first, second])
    expect(client.getQueryData(options.queryKey)).toBeUndefined()
    expect(client.getQueryData(['public', 'config'])).toEqual({ instanceName: 'Test' })
    expect(await client.fetchQuery({ ...options, queryFn: async () => 'new account' })).toBe('new account')
    client.clear()
  })
  it('retries failed reads three times, never retries writes, and retains data on refetch error', async () => {
    const client = createQueryClient()
    let calls = 0
    client.setQueryData(['session', 'test'], ['saved'])
    await expect(client.fetchQuery({ queryKey: ['session', 'test'], staleTime: 0, retryDelay: 0, queryFn: async () => { calls++; throw new Error('offline') } })).rejects.toThrow('offline')
    expect(calls).toBe(4)
    expect(client.getQueryData(['session', 'test'])).toEqual(['saved'])
    expect(client.getDefaultOptions().mutations?.retry).toBe(false)
    expect(client.getDefaultOptions().mutations?.gcTime).toBe(0)
    client.clear()
  })
})
