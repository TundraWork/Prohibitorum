import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import SessionsView from './SessionsView.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}
// ConfirmDialog (reka-ui) teleports to document.body; its confirm button is the
// destructive-variant one carrying the revoke label.
function clickConfirm() {
  const btns = Array.from(document.body.querySelectorAll('button'))
    .filter((b) => b.getAttribute('data-variant') === 'destructive'
      && b.textContent?.includes(en.sessions.revoke))
  btns[btns.length - 1]!.click()
}
const SESSIONS = [
  { id: 's1', isCurrent: true, issuedAt: '', expiresAt: '', lastSeenIp: '10.0.0.1', userAgent: 'Firefox' },
  { id: 's2', isCurrent: false, issuedAt: '', expiresAt: '', lastSeenIp: '10.0.0.2', userAgent: 'Safari' },
]
beforeEach(() => { get.mockReset(); post.mockReset() })

describe('SessionsView', () => {
  it('lists sessions; only non-current rows have a revoke control', async () => {
    get.mockResolvedValue(SESSIONS)
    const wrapper = mount(SessionsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    expect(wrapper.text()).toContain('Firefox')
    expect(wrapper.text()).toContain('Safari')
    expect(wrapper.findAll('[data-test=revoke]')).toHaveLength(1)
  })

  it('per-row action is labelled Revoke (not Sign out) and shows IP address label', async () => {
    get.mockResolvedValue(SESSIONS)
    const wrapper = mount(SessionsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    expect(wrapper.find('[data-test=revoke]').text()).toBe(en.sessions.revoke)
    expect(wrapper.text()).toContain(en.sessions.ipAddress)
  })

  it('revoke is gated behind a confirm dialog (no post until confirmed)', async () => {
    get.mockResolvedValue(SESSIONS)
    post.mockResolvedValue(undefined)
    const wrapper = mount(SessionsView, { global: { plugins: [makeI18n()] }, attachTo: document.body })
    await flushPromises()
    await wrapper.find('[data-test=revoke]').trigger('click')
    await flushPromises()
    expect(post).not.toHaveBeenCalled()
  })

  it('confirming the dialog posts {id} and refreshes', async () => {
    get.mockResolvedValue(SESSIONS)
    post.mockResolvedValue(undefined)
    const wrapper = mount(SessionsView, { global: { plugins: [makeI18n()] }, attachTo: document.body })
    await flushPromises()
    await wrapper.find('[data-test=revoke]').trigger('click')
    await flushPromises()
    clickConfirm()
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/sessions/revoke', { id: 's2' })
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('shows empty state when no sessions are returned', async () => {
    get.mockResolvedValue([])
    const wrapper = mount(SessionsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    expect(wrapper.text()).toContain(en.sessions.empty)
  })

  it('renders error alert when the request fails', async () => {
    get.mockRejectedValue({ code: 'server_error', message: 'boom' })
    const wrapper = mount(SessionsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    expect(wrapper.text()).toContain(en.errors.server_error)
  })
})
