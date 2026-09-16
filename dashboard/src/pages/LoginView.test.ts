import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import en from '@/locales/en'
import LoginView from './LoginView.vue'
import { useSessionExpiry } from '@/composables/useSessionExpiry'

// --- Mocks ----------------------------------------------------------------
vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn() },
}))
import { api } from '@/lib/api'

// vue-router — expose a mutable query so tests can inject route params.
let _routeQuery: Record<string, string> = {}
vi.mock('vue-router', () => ({
  useRoute: () => ({ query: _routeQuery }),
}))

// WebAuthn ceremony: passkeyGet resolves a dummy assertion; never user-cancel.
vi.mock('@/lib/webauthn', () => ({
  passkeyGet: vi.fn(async () => ({ id: 'assert', response: {} })),
  passkeyRegister: vi.fn(),
  isUserCancel: () => false,
}))

// Navigate seam: hardRedirect is used after the server returns a validated redirect.
const { hardRedirect } = vi.hoisted(() => ({ hardRedirect: vi.fn() }))
vi.mock('@/lib/navigate', () => ({ hardRedirect }))

// Return-to navigation: assert the guarded redirect fires (safeReturnTo itself
// is covered by returnTo.test.ts; here we only assert LoginView invokes it).
const { goReturnTo } = vi.hoisted(() => ({ goReturnTo: vi.fn() }))
vi.mock('@/composables/useReturnTo', async () => {
  const { ref, computed } = await import('vue')
  return {
    useReturnTo: () => ({ returnTo: ref('/me'), rawReturnTo: computed(() => _routeQuery.return_to ?? ''), goReturnTo }),
  }
})

// Auth store: `me` is preset per test; `ensureLoaded` is a no-op (the real one
// fetches /me). Default = unauthenticated, so the login methods render.
const authState = vi.hoisted(() => ({ me: null as null | { id: number; username: string }, ensureLoaded: vi.fn(async () => {}) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => authState }))

const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

function mountView() {
  return mount(LoginView, {
    global: {
      plugins: [makeI18n()],
      stubs: { RouterLink: { name: 'RouterLink', props: ['to'], template: '<a><slot/></a>' } },
    },
  })
}

beforeEach(() => {
  setActivePinia(createPinia())
  get.mockReset()
  post.mockReset()
  goReturnTo.mockReset()
  hardRedirect.mockReset()
  authState.me = null
  authState.ensureLoaded.mockClear()
  _routeQuery = {}
  useSessionExpiry().reset()
})

describe('LoginView', () => {
  it('shows a skeleton while the status check is in flight, then renders methods', async () => {
    let resolveStatus!: (v: { bootstrapped: boolean }) => void
    get.mockImplementation((path: string) => {
      if (path === '/api/prohibitorum/auth/status') return new Promise((r) => { resolveStatus = r })
      if (path === '/api/prohibitorum/auth/federation') return Promise.resolve([])
      return Promise.reject(new Error(`unexpected GET ${path}`))
    })
    const wrapper = mountView()
    // Let the auth-store session check resolve so the /auth/status GET fires
    // (its promise stays pending). Status in flight: skeleton up, passkey absent.
    await flushPromises()
    const skeletonBefore = wrapper.find('[role="status"][aria-busy="true"]')
    expect(skeletonBefore.exists(), 'skeleton present while checking').toBe(true)
    expect(
      wrapper.findAll('button').find((b) => b.text().includes(en.login.passkeyButton)),
      'passkey button absent while checking',
    ).toBeFalsy()
    // Resolve status — skeleton should disappear, methods appear.
    resolveStatus({ bootstrapped: true })
    await flushPromises()
    const skeletonAfter = wrapper.find('[role="status"][aria-busy="true"]')
    expect(skeletonAfter.exists(), 'skeleton gone after check').toBe(false)
    expect(
      wrapper.findAll('button').find((b) => b.text().includes(en.login.passkeyButton)),
      'passkey button visible after check',
    ).toBeTruthy()
  })

  it('completes a passkey login and navigates to the server-validated redirect', async () => {
    get.mockImplementation(async (path: string) => {
      if (path === '/api/prohibitorum/auth/status') return { bootstrapped: true }
      if (path === '/api/prohibitorum/auth/federation') return []
      throw new Error(`unexpected GET ${path}`)
    })
    post.mockImplementation(async (path: string) => {
      if (path === '/api/prohibitorum/auth/login/begin') return { challenge: 'abc' }
      if (path.startsWith('/api/prohibitorum/auth/login/complete')) return { redirect: '/resume-here' }
      throw new Error(`unexpected POST ${path}`)
    })

    const wrapper = mountView()
    await flushPromises()

    const passkeyBtn = wrapper
      .findAll('button')
      .find((b) => b.text().includes(en.login.passkeyButton))
    expect(passkeyBtn, 'passkey button present').toBeTruthy()
    await passkeyBtn!.trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/login/begin')
    expect(post).toHaveBeenCalledWith(
      expect.stringContaining('/api/prohibitorum/auth/login/complete?return_to='),
      expect.objectContaining({ id: 'assert' }),
    )
    expect(hardRedirect).toHaveBeenCalledWith('/resume-here')
  })

  it('redirects via return_to when a session already exists (no login form, no status check)', async () => {
    authState.me = { id: 1, username: 'alex' }
    get.mockImplementation(async (path: string) => {
      if (path === '/api/prohibitorum/auth/federation') return []
      throw new Error(`unexpected GET ${path}`)
    })
    const wrapper = mountView()
    await flushPromises()

    expect(goReturnTo).toHaveBeenCalledOnce()
    // No /auth/status call (we short-circuited) and no sign-in methods rendered.
    expect(get).not.toHaveBeenCalledWith('/api/prohibitorum/auth/status')
    expect(
      wrapper.findAll('button').find((b) => b.text().includes(en.login.passkeyButton)),
    ).toBeFalsy()
  })

  it('shows the enroll-admin hint when no admin exists yet', async () => {
    get.mockImplementation(async (path: string) => {
      if (path === '/api/prohibitorum/auth/status') return { bootstrapped: false }
      if (path === '/api/prohibitorum/auth/federation') return []
      throw new Error(`unexpected GET ${path}`)
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain(en.login.noBootstrap)
    const passkeyBtn = wrapper
      .findAll('button')
      .find((b) => b.text().includes(en.login.passkeyButton))
    expect(passkeyBtn).toBeFalsy()
  })

  it('renders the "New device? Pair it" link pointing at /pair', async () => {
    get.mockImplementation(async (path: string) => {
      if (path === '/api/prohibitorum/auth/status') return { bootstrapped: true }
      if (path === '/api/prohibitorum/auth/federation') return []
      throw new Error(`unexpected GET ${path}`)
    })
    const w = mountView()
    await flushPromises()

    const link = w.findComponent({ name: 'RouterLink' })
    expect(link.props('to')).toEqual({ name: 'pair' })
    expect(link.text()).toBe(en.login.pairDevice)
  })

  it('carries the original return_to to device pairing', async () => {
    _routeQuery = { return_to: '/oauth/authorize?client_id=x&state=s' }
    get.mockImplementation(async (path: string) => path.endsWith('/auth/status') ? { bootstrapped: true } : [])
    const w = mountView(); await flushPromises()
    expect(w.findComponent({ name: 'RouterLink' }).props('to')).toEqual({
      name: 'pair', query: { return_to: _routeQuery.return_to },
    })
  })

  it('renders exactly one OrDivider when federation providers are present', async () => {
    get.mockImplementation(async (path: string) => {
      if (path === '/api/prohibitorum/auth/status') return { bootstrapped: true }
      if (path === '/api/prohibitorum/auth/federation') return [{ slug: 'google', displayName: 'Google' }]
      throw new Error(`unexpected GET ${path}`)
    })
    const w = mountView()
    await flushPromises()
    // OrDivider renders with aria-hidden="true" on its root div — use that as the selector.
    // There should be exactly one (the passkey/password divider); FederationButtons no longer adds its own.
    const dividers = w.findAll('[aria-hidden="true"]').filter((el) =>
      el.element.tagName === 'DIV' && el.classes().includes('flex') && el.classes().includes('items-center'),
    )
    expect(dividers).toHaveLength(1)
  })

  it('shows the session-expired notice when reason=session_expired is in the query', async () => {
    _routeQuery = { reason: 'session_expired' }
    get.mockImplementation(async (path: string) => {
      if (path === '/api/prohibitorum/auth/status') return { bootstrapped: true }
      if (path === '/api/prohibitorum/auth/federation') return []
      throw new Error(`unexpected GET ${path}`)
    })
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain(en.login.sessionExpired)
  })

  it('does not show the session-expired notice when the query param is absent', async () => {
    get.mockImplementation(async (path: string) => {
      if (path === '/api/prohibitorum/auth/status') return { bootstrapped: true }
      if (path === '/api/prohibitorum/auth/federation') return []
      throw new Error(`unexpected GET ${path}`)
    })
    const w = mountView()
    await flushPromises()
    expect(w.text()).not.toContain(en.login.sessionExpired)
  })

  it('calls useSessionExpiry().reset() on mount to clear the global banner flag', async () => {
    // Trigger the banner flag first, then mount the login view — it should reset it.
    useSessionExpiry().trigger()
    expect(useSessionExpiry().expired.value).toBe(true)
    get.mockImplementation(async (path: string) => {
      if (path === '/api/prohibitorum/auth/status') return { bootstrapped: true }
      if (path === '/api/prohibitorum/auth/federation') return []
      throw new Error(`unexpected GET ${path}`)
    })
    mountView()
    await flushPromises()
    expect(useSessionExpiry().expired.value).toBe(false)
  })
})


describe('LoginView — forward-auth explanation', () => {
  const contextPath = '/api/prohibitorum/forward-auth/login-context?return_to='
  function responses(application: { label: string } | null) {
    get.mockImplementation(async (path: string) => {
      if (path.startsWith(contextPath)) return { application }
      if (path.endsWith('/auth/status')) return { bootstrapped: true }
      if (path.endsWith('/auth/federation')) return []
      throw new Error('Unexpected request')
    })
  }

  it.each(['Registered app', 'app.example:8443', '<script>display as text</script>'])(
    'renders the server label as text: %s', async (label) => {
      _routeQuery = { return_to: 'https://idp.example/oauth/authorize?client_id=app&state=x' }
      responses({ label })
      const w = mountView(); await flushPromises()
      expect(w.get('[data-test="forward-auth-context"]').text()).toContain(label)
      expect(w.get('[data-test="forward-auth-context"]').text()).toContain('Prohibitorum provides sign-in')
      expect(w.find('[data-test="forward-auth-context"] script').exists()).toBe(false)
      expect(get).toHaveBeenCalledWith(contextPath + encodeURIComponent(_routeQuery.return_to!))
      expect(w.findComponent({ name: 'RouterLink' }).props('to').query.return_to).toBe(_routeQuery.return_to)
      w.unmount()
    },
  )

  it('does not request context on direct login or before redirecting an existing session', async () => {
    responses(null)
    let w = mountView(); await flushPromises(); w.unmount()
    _routeQuery = { return_to: 'https://idp.example/oauth/authorize?client_id=app' }
    authState.me = { id: 1, username: 'alex' }
    w = mountView(); await flushPromises()
    expect(get.mock.calls.some(([path]) => path.startsWith(contextPath))).toBe(false)
    expect(goReturnTo).toHaveBeenCalledOnce()
    w.unmount()
  })

  it.each(['absent', 'failed', 'pending'])('keeps login usable when context is %s', async (mode) => {
    _routeQuery = { return_to: 'https://idp.example/oauth/authorize?client_id=app' }
    responses(null)
    const normal = get.getMockImplementation()!
    get.mockImplementation((path) => {
      if (path.startsWith(contextPath) && mode === 'failed') return Promise.reject(new Error('unavailable'))
      if (path.startsWith(contextPath) && mode === 'pending') return new Promise(() => {})
      return normal(path)
    })
    const w = mountView(); await flushPromises()
    expect(w.find('[data-test="forward-auth-context"]').exists()).toBe(false)
    expect(w.text()).toContain(en.login.passkeyButton)
    expect(w.text()).toContain(en.login.passwordLabel)
    w.unmount()
  })

  it('ignores an old response after the destination changes', async () => {
    _routeQuery = reactive({ return_to: 'https://idp.example/oauth/authorize?client_id=first' })
    let oldResponse!: (value: unknown) => void
    responses({ label: 'Second app' })
    const normal = get.getMockImplementation()!
    get.mockImplementation((path) => {
      if (path.startsWith(contextPath) && decodeURIComponent(path).includes('client_id=first')) {
        return new Promise(resolve => { oldResponse = resolve })
      }
      return normal(path)
    })
    const w = mountView(); await flushPromises()
    _routeQuery.return_to = 'https://idp.example/oauth/authorize?client_id=second'
    await flushPromises()
    expect(w.text()).toContain('Second app')
    oldResponse({ application: { label: 'First app' } }); await flushPromises()
    expect(w.text()).not.toContain('First app')
    expect(w.text()).toContain('Second app')
    w.unmount()
  })
})
