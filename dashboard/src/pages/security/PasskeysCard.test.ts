import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import PasskeysCard from './PasskeysCard.vue'

const get = vi.fn(); const post = vi.fn()
vi.mock('../../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a) } }))
const add = vi.fn()
vi.mock('../../lib/webauthn', () => ({ passkeyAddCredential: (...a: unknown[]) => add(...a) }))

beforeEach(() => { get.mockReset(); post.mockReset(); add.mockReset() })
const creds = [{ id: 1, credentialIdSuffix: 'ab12', nickname: 'Laptop', transports: ['internal'], createdAt: '2026-01-01T00:00:00Z' }]

describe('PasskeysCard', () => {
  it('lists credentials', async () => {
    get.mockResolvedValueOnce(creds)
    const w = mount(PasskeysCard); await flushPromises()
    expect(w.findAll('tbody tr').length).toBe(1)
    expect(w.text()).toContain('Laptop')
  })
  it('adds a passkey then refetches', async () => {
    get.mockResolvedValueOnce(creds)
    add.mockResolvedValueOnce({ id: 2 })
    get.mockResolvedValueOnce([...creds, { id: 2, credentialIdSuffix: 'cd34', nickname: null, transports: [], createdAt: '2026-01-02T00:00:00Z' }])
    const w = mount(PasskeysCard); await flushPromises()
    await w.find('[data-test="add-passkey"]').trigger('click'); await flushPromises()
    expect(add).toHaveBeenCalled()
    expect(get).toHaveBeenCalledTimes(2)
  })
})
