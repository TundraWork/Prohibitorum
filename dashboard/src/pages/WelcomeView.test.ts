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
  it('renders the identity and shows Continue enabled even while avatar is pending', async () => {
    get.mockResolvedValueOnce({ idpDisplayName: 'Google', displayName: 'Jane Doe', username: 'jane', email: 'jane@x.com', avatarPending: true })
       .mockResolvedValueOnce({ idpDisplayName: 'Google', displayName: 'Jane Doe', username: 'jane', email: 'jane@x.com', avatarUrl: '/avatar/x?v=ab', avatarPending: false })
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Jane Doe')
    expect(w.text()).toContain('jane@x.com')
    const cont = w.find('[data-test="welcome-continue"]')
    // Continue must NOT be disabled while avatar is still pending (avatar is a background nicety)
    expect((cont.element as HTMLButtonElement).disabled).toBe(false)
    // Body always shows the description (not the fetching message)
    expect(w.text()).toContain('Confirm this is the account you want to connect.')
    // Advance fake timers past the poll interval so the second GET fires
    await vi.advanceTimersByTimeAsync(20)
    await flushPromises()
    expect(get).toHaveBeenCalledTimes(2)
    expect((cont.element as HTMLButtonElement).disabled).toBe(false)
  })

  it('does not show the fetching-avatar text in the body (only on spinner overlay)', async () => {
    get.mockResolvedValue({ idpDisplayName: 'Google', displayName: 'Jane', username: 'jane', email: 'j@x.com', avatarPending: true })
    const w = mountView()
    await flushPromises()
    // The description should always be shown
    expect(w.text()).toContain('Confirm this is the account you want to connect.')
    // The fetching text may appear in aria-label on the overlay but should not
    // duplicate in the body aria-live region
    const bodyLive = w.find('[aria-live="polite"]')
    expect(bodyLive.text()).toBe('Confirm this is the account you want to connect.')
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

  it('stops polling after cap is exhausted with avatarPending always true', async () => {
    // Always returns avatarPending:true so the poll never naturally settles
    get.mockResolvedValue({ idpDisplayName: 'Google', displayName: 'Jane', username: 'jane', email: 'j@x.com', avatarPending: true })
    // pollMs=20, capMs=40: after two polls (40ms elapsed) the cap is hit
    const w = mountView(20, 40)
    await flushPromises()
    const cont = w.find('[data-test="welcome-continue"]')
    // Continue is enabled immediately (avatar pending does not block it)
    expect((cont.element as HTMLButtonElement).disabled).toBe(false)
    // First poll fires at 20ms (elapsed becomes 20, schedules next)
    await vi.advanceTimersByTimeAsync(20)
    await flushPromises()
    const callsAfterFirst = get.mock.calls.length
    // Second poll fires at 40ms (elapsed becomes 40, >= capMs, no more polls)
    await vi.advanceTimersByTimeAsync(20)
    await flushPromises()
    await vi.advanceTimersByTimeAsync(100)
    await flushPromises()
    // No more polls after cap — call count should have stopped growing
    expect(get.mock.calls.length).toBe(callsAfterFirst + 1)
    expect((cont.element as HTMLButtonElement).disabled).toBe(false)
  })
})
