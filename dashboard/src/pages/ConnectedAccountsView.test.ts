import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ConnectedAccountsView from './ConnectedAccountsView.vue'

const get = vi.fn(); const post = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a) } }))
vi.mock('../lib/sudo', () => ({ withSudo: (fn: any) => fn(), ensureSudo: vi.fn().mockResolvedValue(true) }))
beforeEach(() => { get.mockReset(); post.mockReset() })

const idents = [{ id: 5, idpSlug: 'mockop', idpDisplayName: 'Mock OP', upstreamEmail: 'a@x', linkedAt: '2026-01-01T00:00:00Z' }]
const providers = [{ slug: 'mockop', displayName: 'Mock OP' }, { slug: 'other', displayName: 'Other' }]

describe('ConnectedAccountsView', () => {
  it('lists identities and providers', async () => {
    get.mockResolvedValueOnce(idents)     // /me/identities
    get.mockResolvedValueOnce(providers)  // /auth/federation
    const w = mount(ConnectedAccountsView); await flushPromises()
    expect(w.text()).toContain('Mock OP')
    expect(w.findAll('tbody tr').length).toBe(1)
  })
  it('unlinks via withSudo then refetches', async () => {
    get.mockResolvedValueOnce(idents); get.mockResolvedValueOnce(providers)
    post.mockResolvedValueOnce(undefined)
    get.mockResolvedValueOnce([]); get.mockResolvedValueOnce(providers)
    const w = mount(ConnectedAccountsView); await flushPromises()
    await w.find('[data-test="unlink"]').trigger('click')
    await w.find('[data-test="unlink"]').trigger('click') // arm+confirm
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/identities/5/unlink')
  })
})
