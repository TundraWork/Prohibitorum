import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import en from '../locales/en'
import InvitationsView from './InvitationsView.vue'

const get = vi.fn()
const post = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a) } }))

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}
beforeEach(() => { get.mockReset(); post.mockReset() })

const invites = [
  { token: 'tok1', url: 'http://x/enroll/tok1', role: 'user', createdAt: '2026-01-01T00:00:00Z', expiresAt: '2026-02-01T00:00:00Z' },
]

describe('InvitationsView', () => {
  it('lists invitations with their urls', async () => {
    get.mockResolvedValueOnce(invites)
    const wrapper = mount(InvitationsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    expect(wrapper.findAll('tbody tr').length).toBe(1)
    // CopyableUrl binds the url into a readonly <input> value (not text content).
    const inputs = wrapper.findAll('input')
    expect(inputs.some((i) => (i.element as HTMLInputElement).value === 'http://x/enroll/tok1')).toBe(true)
  })

  it('creates an invitation then refetches', async () => {
    get.mockResolvedValueOnce([])                                  // initial
    post.mockResolvedValueOnce({ url: 'http://x/enroll/new', expiresAt: '2026-02-01T00:00:00Z' })
    get.mockResolvedValueOnce(invites)                             // refetch
    const wrapper = mount(InvitationsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    await wrapper.find('[data-test="create"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations', { role: 'user' })
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('revokes by token', async () => {
    get.mockResolvedValueOnce(invites)
    post.mockResolvedValueOnce(undefined)
    get.mockResolvedValueOnce([])
    const wrapper = mount(InvitationsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    await wrapper.find('[data-test="revoke"]').trigger('click') // arm
    await wrapper.find('[data-test="revoke"]').trigger('click') // confirm
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations/revoke', { token: 'tok1' })
  })
})
