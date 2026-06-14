import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import WelcomeView from './WelcomeView.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

const assignSpy = vi.fn()
// jsdom location.assign
Object.defineProperty(window, 'location', { value: { assign: assignSpy, href: '' }, writable: true })

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
function mountView(pollMs = 20, capMs?: number) {
  const props: Record<string, number> = { pollMs }
  if (capMs !== undefined) props.capMs = capMs
  return mount(WelcomeView, { global: { plugins: [i18n()] }, props })
}

beforeEach(() => {
  get.mockReset()
  post.mockReset()
  assignSpy.mockReset()
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('WelcomeView', () => {
  it('renders the identity and gates Continue until the avatar settles', async () => {
    get.mockResolvedValueOnce({ idpDisplayName: 'Google', displayName: 'Jane Doe', username: 'jane', email: 'jane@x.com', avatarPending: true })
       .mockResolvedValueOnce({ idpDisplayName: 'Google', displayName: 'Jane Doe', username: 'jane', email: 'jane@x.com', avatarUrl: '/avatar/x?v=ab', avatarPending: false })
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Jane Doe')
    expect(w.text()).toContain('jane@x.com')
    const cont = w.find('[data-test="welcome-continue"]')
    expect((cont.element as HTMLButtonElement).disabled).toBe(true)
    // Advance fake timers past the poll interval so the second GET fires
    await vi.advanceTimersByTimeAsync(20)
    await flushPromises()
    expect(get).toHaveBeenCalledTimes(2)
    expect((cont.element as HTMLButtonElement).disabled).toBe(false)
  })

  it('confirms and navigates on Continue', async () => {
    get.mockResolvedValue({ idpDisplayName: 'Google', displayName: 'Jane', username: 'jane', email: 'j@x.com', avatarPending: false })
    post.mockResolvedValueOnce({ redirect: '/' })
    const w = mountView()
    await flushPromises()
    await w.find('[data-test="welcome-continue"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/federation/confirm', expect.anything())
    expect(assignSpy).toHaveBeenCalledWith('/')
  })

  it('declines on Not me', async () => {
    get.mockResolvedValue({ idpDisplayName: 'Google', displayName: 'Jane', username: 'jane', email: 'j@x.com', avatarPending: false })
    post.mockResolvedValue({})
    const w = mountView()
    await flushPromises()
    await w.find('[data-test="welcome-notme"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/federation/confirm/decline', expect.anything())
    expect(assignSpy).toHaveBeenCalledWith('/login')
  })

  it('redirects to /login on 401 during initial GET', async () => {
    get.mockRejectedValueOnce({ code: 'no_session' })
    mountView()
    await flushPromises()
    expect(assignSpy).toHaveBeenCalledWith('/login')
  })

  it('surfaces confirmError and re-enables Continue when confirm() POST fails', async () => {
    get.mockResolvedValue({ idpDisplayName: 'Google', displayName: 'Jane', username: 'jane', email: 'j@x.com', avatarPending: false })
    post.mockRejectedValueOnce({ code: 'server_error' })
    const w = mountView()
    await flushPromises()
    const cont = w.find('[data-test="welcome-continue"]')
    await cont.trigger('click')
    await flushPromises()
    // Error message should be visible
    expect(w.text()).toContain('Could not confirm')
    // No navigation
    expect(assignSpy).not.toHaveBeenCalled()
    // Continue re-enabled
    expect((cont.element as HTMLButtonElement).disabled).toBe(false)
  })

  it('settles Continue after cap is exhausted with avatarPending always true', async () => {
    // Always returns avatarPending:true so the poll never naturally settles
    get.mockResolvedValue({ idpDisplayName: 'Google', displayName: 'Jane', username: 'jane', email: 'j@x.com', avatarPending: true })
    // pollMs=20, capMs=40: after two polls (40ms elapsed) the cap is hit
    const w = mountView(20, 40)
    await flushPromises()
    const cont = w.find('[data-test="welcome-continue"]')
    expect((cont.element as HTMLButtonElement).disabled).toBe(true)
    // First poll fires at 20ms (elapsed becomes 20, schedules next)
    await vi.advanceTimersByTimeAsync(20)
    await flushPromises()
    // Second poll fires at 40ms (elapsed becomes 40, >= capMs, settled)
    await vi.advanceTimersByTimeAsync(20)
    await flushPromises()
    expect((cont.element as HTMLButtonElement).disabled).toBe(false)
  })
})
