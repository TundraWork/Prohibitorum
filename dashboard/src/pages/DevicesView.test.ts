import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import DevicesView from './DevicesView.vue'

const get = vi.fn(); const post = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a) } }))
vi.mock('../lib/sudo', () => ({ withSudo: (fn: any) => fn() }))
beforeEach(() => { get.mockReset(); post.mockReset() })

const pending = { pairingId: 'p1', displayCode: 'ABCD-1234', initiatorUa: 'CLI', initiatorIp: '1.1.1.1', createdAt: '2026-01-01T00:00:00Z', expiresAt: '2026-01-01T00:10:00Z', alreadyBound: false }

describe('DevicesView', () => {
  it('looks up a pending pairing then approves', async () => {
    get.mockResolvedValueOnce(pending)
    post.mockResolvedValueOnce(undefined)
    const w = mount(DevicesView)
    await w.find('[data-test="code"]').setValue('ABCD-1234')
    await w.find('[data-test="lookup"]').trigger('click'); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/me/devices/pair/lookup?code=ABCD-1234')
    expect(w.text()).toContain('CLI')
    await w.find('[data-test="approve"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/devices/pair/approve', { code: 'ABCD-1234' })
  })
})
