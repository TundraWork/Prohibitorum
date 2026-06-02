import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import AccountDetailView from './AccountDetailView.vue'

const get = vi.fn(); const post = vi.fn(); const put = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a:unknown[])=>get(...a), post: (...a:unknown[])=>post(...a), put: (...a:unknown[])=>put(...a) } }))
beforeEach(() => { get.mockReset(); post.mockReset(); put.mockReset() })

const acct = { id: 2, username: 'bob', displayName: 'Bob', role: 'user', disabled: false, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' }
function makeRouter() { return createRouter({ history: createMemoryHistory(), routes: [
  { path: '/admin/accounts/:id', component: AccountDetailView }, { path: '/admin/accounts', component: { template: '<div/>' } } ] }) }
async function mountAt() { const r = makeRouter(); r.push('/admin/accounts/2'); await r.isReady(); return mount(AccountDetailView, { global: { plugins: [r] } }) }

describe('AccountDetailView', () => {
  it('loads and saves role/disabled', async () => {
    get.mockResolvedValueOnce(acct)
    put.mockResolvedValueOnce({ ...acct, role: 'admin' })
    const w = await mountAt(); await flushPromises()
    expect(w.text()).toContain('bob')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/accounts/2', { displayName: 'Bob', role: 'user', disabled: false })
  })
  it('revoke sessions shows the count', async () => {
    get.mockResolvedValueOnce(acct)
    post.mockResolvedValueOnce({ revoked: 3 })
    const w = await mountAt(); await flushPromises()
    await w.find('[data-test="revoke-sessions"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/accounts/revoke-sessions', { id: 2 })
    expect(w.text()).toContain('3')
  })
})
