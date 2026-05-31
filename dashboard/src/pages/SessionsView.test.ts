import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import en from '../locales/en'
import SessionsView from './SessionsView.vue'

const get = vi.fn()
const post = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a) } }))

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}

beforeEach(() => { get.mockReset(); post.mockReset() })

const rows = [
  { id: 'cur', isCurrent: true, issuedAt: '2026-01-01T00:00:00Z', expiresAt: '2026-02-01T00:00:00Z', lastSeenIp: '1.1.1.1', userAgent: 'Cur' },
  { id: 'other', isCurrent: false, issuedAt: '2026-01-01T00:00:00Z', expiresAt: '2026-02-01T00:00:00Z', lastSeenIp: '2.2.2.2', userAgent: 'Other' },
]

describe('SessionsView', () => {
  it('lists sessions; only non-current rows are revocable', async () => {
    get.mockResolvedValueOnce(rows)
    const wrapper = mount(SessionsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    expect(wrapper.findAll('tbody tr').length).toBe(2)
    const revokeButtons = wrapper.findAll('[data-test="revoke"]')
    expect(revokeButtons.length).toBe(1)
  })

  it('revokes a non-current session then refetches', async () => {
    get.mockResolvedValueOnce(rows)        // initial
    post.mockResolvedValueOnce(undefined)  // revoke
    get.mockResolvedValueOnce([rows[0]])   // refetch
    const wrapper = mount(SessionsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    await wrapper.find('[data-test="revoke"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/sessions/revoke', { id: 'other' })
    expect(get).toHaveBeenCalledTimes(2)
  })
})
