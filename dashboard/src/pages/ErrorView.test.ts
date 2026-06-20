import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia } from 'pinia'
import en from '@/locales/en'
import ErrorView from './ErrorView.vue'

// vue-router — expose a mutable query so tests can inject route params.
let _routeQuery: Record<string, string> = {}
vi.mock('vue-router', () => ({
  useRoute: () => ({ query: _routeQuery }),
}))

// Auth store: `me` is preset per test; `ensureLoaded` is controllable.
const authState = vi.hoisted(() => ({
  me: null as null | { id: number; username: string },
  ensureLoaded: vi.fn(async () => {}),
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => authState }))

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

function mountView() {
  return mount(ErrorView, {
    global: {
      plugins: [makeI18n(), createPinia()],
      stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } },
    },
  })
}

beforeEach(() => {
  _routeQuery = {}
  authState.me = null
  authState.ensureLoaded.mockClear()
  authState.ensureLoaded.mockResolvedValue(undefined)
})

describe('ErrorView', () => {
  it('renders the error message from the errors.* i18n map', async () => {
    _routeQuery = { error: 'upstream_error' }
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain(en.errors.upstream_error)
  })

  it('renders the reference line when ref is present', async () => {
    _routeQuery = { error: 'upstream_error', ref: 'abc123' }
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain(en.errors.upstream_error)
    expect(wrapper.text()).toContain('Reference: abc123')
    // Unauthenticated: button should be returnToLogin → /login
    const link = wrapper.find('a')
    expect(link.attributes('href')).toBe('/login')
    expect(link.text()).toBe(en.error.returnToLogin)
  })

  it('omits the reference line when ref is absent', async () => {
    _routeQuery = { error: 'upstream_error' }
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).not.toContain('Reference:')
  })

  it('shows Back to dashboard when authenticated', async () => {
    authState.me = { id: 1, username: 'alex' }
    _routeQuery = { error: 'server_error' }

    const wrapper = mountView()
    await flushPromises()

    const link = wrapper.find('a')
    expect(link.attributes('href')).toBe('/security')
    expect(link.text()).toBe(en.error.backToDashboard)
  })

  it('shows Return to sign in when not authenticated', async () => {
    authState.me = null
    _routeQuery = { error: 'server_error' }

    const wrapper = mountView()
    await flushPromises()

    const link = wrapper.find('a')
    expect(link.attributes('href')).toBe('/login')
    expect(link.text()).toBe(en.error.returnToLogin)
  })

  it('ignores ensureLoaded errors and treats user as unauthenticated', async () => {
    authState.ensureLoaded.mockRejectedValue(new Error('network error'))
    authState.me = null
    _routeQuery = { error: 'server_error' }

    const wrapper = mountView()
    await flushPromises()

    const link = wrapper.find('a')
    expect(link.attributes('href')).toBe('/login')
    expect(link.text()).toBe(en.error.returnToLogin)
  })
})
