import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
vi.mock('@/lib/webauthn', () => ({
  passkeyGet: vi.fn(), isUserCancel: () => false,
  passkeyRegister: vi.fn(async () => ({ id: 'newcred', response: {} })),
}))

import PairDeviceView from './PairDeviceView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(PairDeviceView, { global: { plugins: [i18n()] }, attachTo: document.body })

const BEGIN = { pairingId: 'p1', code: 'AB12CD34', displayCode: 'AB12-CD34', expiresAt: '2999-01-01T00:00:00Z' }

beforeEach(() => { setActivePinia(createPinia()); get.mockReset(); post.mockReset(); push.mockReset(); vi.useFakeTimers() })
afterEach(() => { vi.useRealTimers() })

describe('PairDeviceView', () => {
  it('begins on mount and shows the display code as large mono display', async () => {
    post.mockResolvedValue(BEGIN)
    get.mockResolvedValue({ status: 'pending' })
    const w = mountView(); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/devices/pair/begin')
    expect(w.text()).toContain('AB12-CD34')
    expect(w.text()).toContain(en.pair.waiting)
    // Code renders as the large display element, not a CodeField copy widget
    const displayEl = w.find('[data-test="display-code"]')
    expect(displayEl.exists()).toBe(true)
    expect(displayEl.text()).toBe('AB12-CD34')
    // No copy button should be present for the display code
    const copyBtns = w.findAll('button').filter((b) => b.text() === en.common.copy || b.attributes('aria-label')?.includes('opy'))
    expect(copyBtns).toHaveLength(0)
  })

  it('polls; on approved it completes and shows success', async () => {
    post.mockImplementation(async (p: string) =>
      p.endsWith('/pair/begin') ? BEGIN : { session: { role: 'user' } })
    get.mockResolvedValueOnce({ status: 'pending' }).mockResolvedValue({ status: 'approved' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600)
    await vi.advanceTimersByTimeAsync(2600)
    await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/auth/devices/pair/status?id=p1')
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/devices/pair/complete', { pairingId: 'p1' })
    expect(w.text()).toContain(en.pair.success)
  })

  it('shows expired state and regenerates on click', async () => {
    post.mockResolvedValue(BEGIN)
    get.mockResolvedValue({ status: 'expired' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600)
    await flushPromises()
    expect(w.text()).toContain(en.pair.expired)
    post.mockClear()
    await w.find('[data-test="regenerate"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/devices/pair/begin')
  })

  it('skip on success navigates to dashboard', async () => {
    post.mockImplementation(async (p: string) =>
      p.endsWith('/pair/begin') ? BEGIN : { session: { role: 'user' } })
    get.mockResolvedValue({ status: 'approved' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600); await flushPromises()
    await w.find('[data-test="skip"]').trigger('click'); await flushPromises()
    expect(push).toHaveBeenCalledWith('/')
  })

  it('shows skip-safe note in the success phase', async () => {
    post.mockImplementation(async (p: string) =>
      p.endsWith('/pair/begin') ? BEGIN : { session: { role: 'user' } })
    get.mockResolvedValue({ status: 'approved' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600); await flushPromises()
    expect(w.text()).toContain(en.pair.skipSafe)
  })

  it('add-passkey registers then navigates to dashboard', async () => {
    post.mockImplementation(async (p: string) => {
      if (p.endsWith('/pair/begin')) return BEGIN
      if (p.endsWith('/pair/complete')) return { session: { role: 'user' } }
      if (p.endsWith('/register/begin')) return { challenge: 'c' }
      return undefined
    })
    get.mockResolvedValue({ status: 'approved' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600); await flushPromises()
    await w.find('[data-test="add-passkey"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/credentials/register/begin')
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/credentials/register/complete',
      expect.objectContaining({ id: 'newcred' }))
    expect(push).toHaveBeenCalledWith('/')
  })

  it('does not complete after the component unmounts mid-poll', async () => {
    post.mockImplementation(async (p: string) =>
      p.endsWith('/pair/begin') ? BEGIN : { session: { role: 'user' } })
    let resolveStatus!: (v: { status: string }) => void
    get.mockReturnValueOnce(new Promise((r) => { resolveStatus = r }))
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600) // fires the first poll; its GET is now in-flight
    w.unmount()
    resolveStatus({ status: 'approved' })
    await flushPromises()
    expect(post).not.toHaveBeenCalledWith(
      '/api/prohibitorum/auth/devices/pair/complete', expect.anything())
  })

  it('resumes polling and retries when complete fails once', async () => {
    let completeCalls = 0
    post.mockImplementation(async (p: string) => {
      if (p.endsWith('/pair/begin')) return BEGIN
      if (p.endsWith('/pair/complete')) {
        completeCalls++
        if (completeCalls === 1) throw { code: 'server_error', message: 'boom' }
        return { session: { role: 'user' } }
      }
      return undefined
    })
    get.mockResolvedValue({ status: 'approved' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600); await flushPromises() // poll1 → approved → complete fails → resume
    expect(w.text()).toContain(en.pair.waiting) // still pending, not wedged
    await vi.advanceTimersByTimeAsync(2600); await flushPromises() // poll2 → approved → complete succeeds
    expect(completeCalls).toBe(2)
    expect(w.text()).toContain(en.pair.success)
  })

  it('shows an error if passkey registration fails (and does not navigate)', async () => {
    const { passkeyRegister } = await import('@/lib/webauthn')
    vi.mocked(passkeyRegister).mockRejectedValueOnce(new Error('boom'))
    post.mockImplementation(async (p: string) => {
      if (p.endsWith('/pair/begin')) return BEGIN
      if (p.endsWith('/pair/complete')) return { session: { role: 'user' } }
      if (p.endsWith('/register/begin')) return { challenge: 'c' }
      return undefined
    })
    get.mockResolvedValue({ status: 'approved' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600); await flushPromises()
    await w.find('[data-test="add-passkey"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.errors.webauthn_error)
    expect(push).not.toHaveBeenCalled()
  })
})
