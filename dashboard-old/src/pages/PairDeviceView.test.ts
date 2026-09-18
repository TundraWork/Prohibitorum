import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

const { routeQuery, hardRedirect } = vi.hoisted(() => ({
  routeQuery: {} as Record<string, string>, hardRedirect: vi.fn(),
}))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: routeQuery }) }))
vi.mock('@/lib/navigate', () => ({ hardRedirect }))
vi.mock('@/lib/webauthn', () => ({
  passkeyGet: vi.fn(), isUserCancel: () => false,
  passkeyRegister: vi.fn(async () => ({ id: 'newcred', response: {} })),
}))

import PairDeviceView from './PairDeviceView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(PairDeviceView, { global: { plugins: [i18n()] }, attachTo: document.body })

const BEGIN = { pairingId: 'p1', code: 'AB12CD34', displayCode: 'AB12-CD34', expiresAt: '2999-01-01T00:00:00Z' }


beforeEach(() => {
  get.mockReset(); post.mockReset(); hardRedirect.mockReset(); vi.useFakeTimers()
  for (const key of Object.keys(routeQuery)) delete routeQuery[key]
})
afterEach(() => { vi.useRealTimers() })

describe('PairDeviceView', () => {
  for (const action of ['skip', 'add-passkey']) {
    it(`continues to the server-issued destination after ${action}`, async () => {
      routeQuery.return_to = '/oauth/authorize?client_id=x&state=original'
      const destination = '/oauth/authorize?client_id=x&state=validated'
      post.mockImplementation(async (path: string) => {
        if (path.endsWith('/pair/begin')) return BEGIN
        if (path.includes('/pair/complete')) return { redirect: destination }
        if (path.endsWith('/register/begin')) return { challenge: 'c' }
        return {}
      })
      get.mockResolvedValue({ status: 'approved' })
      const w = mountView(); await flushPromises()
      await vi.advanceTimersByTimeAsync(2600); await flushPromises()
      expect(post).toHaveBeenCalledWith(
        `/api/prohibitorum/auth/devices/pair/complete?return_to=${encodeURIComponent(routeQuery.return_to)}`,
        { pairingId: 'p1' })
      expect(w.find('[data-test=skip]').text()).toBe(en.pair.continue)
      await w.find(`[data-test=${action}]`).trigger('click'); await flushPromises()
      expect(hardRedirect).toHaveBeenCalledWith(destination)
    })
  }

  it('uses dashboard wording when the server rejects an unsafe destination', async () => {
    routeQuery.return_to = 'https://evil.example/'
    post.mockImplementation(async (path: string) => path.endsWith('/pair/begin') ? BEGIN : { redirect: '/' })
    get.mockResolvedValue({ status: 'approved' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600); await flushPromises()
    expect(w.find('[data-test=skip]').text()).toBe(en.pair.skip)
    await w.find('[data-test=skip]').trigger('click')
    expect(hardRedirect).toHaveBeenCalledWith('/')
  })

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
      p.endsWith('/pair/begin') ? BEGIN : { session: { role: 'user' }, redirect: '/' })
    get.mockResolvedValueOnce({ status: 'pending' }).mockResolvedValue({ status: 'approved' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600)
    await vi.advanceTimersByTimeAsync(2600)
    await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/auth/devices/pair/status?id=p1', expect.objectContaining({ signal: expect.any(AbortSignal) }))
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
      p.endsWith('/pair/begin') ? BEGIN : { session: { role: 'user' }, redirect: '/' })
    get.mockResolvedValue({ status: 'approved' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600); await flushPromises()
    await w.find('[data-test="skip"]').trigger('click'); await flushPromises()
    expect(hardRedirect).toHaveBeenCalledWith('/')
  })

  it('shows skip-safe note in the success phase', async () => {
    post.mockImplementation(async (p: string) =>
      p.endsWith('/pair/begin') ? BEGIN : { session: { role: 'user' }, redirect: '/' })
    get.mockResolvedValue({ status: 'approved' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600); await flushPromises()
    expect(w.text()).toContain(en.pair.skipSafe)
  })

  it('add-passkey registers then navigates to dashboard', async () => {
    post.mockImplementation(async (p: string) => {
      if (p.endsWith('/pair/begin')) return BEGIN
      if (p.endsWith('/pair/complete')) return { session: { role: 'user' }, redirect: '/' }
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
    expect(hardRedirect).toHaveBeenCalledWith('/')
  })

  it('does not complete after the component unmounts mid-poll', async () => {
    post.mockImplementation(async (p: string) =>
      p.endsWith('/pair/begin') ? BEGIN : { session: { role: 'user' }, redirect: '/' })
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

  it('does not replay failed completion until the user explicitly retries', async () => {
    let completeCalls = 0
    post.mockImplementation(async (p: string) => {
      if (p.endsWith('/pair/begin')) return BEGIN
      if (p.endsWith('/pair/complete')) {
        completeCalls++
        if (completeCalls === 1) throw { code: 'server_error', message: 'boom' }
        return { session: { role: 'user' }, redirect: '/' }
      }
      return undefined
    })
    get.mockResolvedValue({ status: 'approved' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600); await flushPromises() // poll1 → approved → complete fails → resume
    expect(w.text()).toContain(en.pair.waiting) // still pending, not wedged
    await vi.advanceTimersByTimeAsync(10000); await flushPromises()
    expect(completeCalls).toBe(1)
    await w.get('[data-test=retry-complete]').trigger('click'); await flushPromises()
    expect(completeCalls).toBe(2)
    expect(w.text()).toContain(en.pair.success)
  })

  it('shows an error if passkey registration fails (and does not navigate)', async () => {
    const { passkeyRegister } = await import('@/lib/webauthn')
    vi.mocked(passkeyRegister).mockRejectedValueOnce(new Error('boom'))
    post.mockImplementation(async (p: string) => {
      if (p.endsWith('/pair/begin')) return BEGIN
      if (p.endsWith('/pair/complete')) return { session: { role: 'user' }, redirect: '/' }
      if (p.endsWith('/register/begin')) return { challenge: 'c' }
      return undefined
    })
    get.mockResolvedValue({ status: 'approved' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600); await flushPromises()
    await w.find('[data-test="add-passkey"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.errors.codes.webauthn_error)
    expect(hardRedirect).not.toHaveBeenCalled()
  })
})
