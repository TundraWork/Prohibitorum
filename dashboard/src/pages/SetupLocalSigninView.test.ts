import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createRouter, createMemoryHistory, type Router } from 'vue-router'
import en from '@/locales/en'
import SetupLocalSigninView from './SetupLocalSigninView.vue'

// Shared layout appearance is covered in the browser; keep these flow tests
// independent of the layout's session/config data provider.
vi.mock('./CenteredLayout.vue', () => ({
  default: { template: '<div><slot /></div>' },
}))

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'

const post = vi.mocked(api.post)

vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => unknown) => fn(), ensureSudo: vi.fn(), sudoState: { value: { open: false, resolve: null } }, _resolveSudo: vi.fn() }))

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

async function makeRouter(query: Record<string, string> = {}): Promise<Router> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'home', component: { template: '<div/>' } },
      { path: '/setup-signin', name: 'setup-signin', component: { template: '<div/>' } },
    ],
  })
  router.push({ path: '/setup-signin', query })
  await router.isReady()
  return router
}

async function mountView(query: Record<string, string> = {}) {
  const wrapper = mount(SetupLocalSigninView, {
    global: { plugins: [await makeRouter(query), makeI18n()] },
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  post.mockReset()
})

describe('SetupLocalSigninView', () => {
  it('setting a password posts it and reports success', async () => {
    post.mockResolvedValue({})
    const wrapper = await mountView()

    await wrapper.get('[data-test="choose-password"]').trigger('click')
    await wrapper.get('#setup-pw-new').setValue('correct horse battery staple')
    await wrapper.get('#setup-pw-confirm').setValue('correct horse battery staple')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/password/set', {
      password: 'correct horse battery staple',
    })
    expect(wrapper.find('[data-test="password-done"]').exists()).toBe(true)
  })

  it('setting TOTP verifies the code and offers the finish button', async () => {
    post.mockImplementation(async (path: string) => {
      if (path.endsWith('/totp/begin')) return { secret_base32: 'JBSWY3DP', otpauth_uri: 'otpauth://totp/x' }
      if (path.endsWith('/totp/verify')) return { recovery_codes: ['1111-2222', '3333-4444'] }
      throw new Error(`unexpected POST ${path}`)
    })
    const wrapper = await mountView()

    await wrapper.get('[data-test="choose-totp"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/totp/begin')

    await wrapper.get('#setup-totp-code').setValue('123456')
    await wrapper.findAll('button').filter((b) => b.text() === 'Verify')[0].trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/totp/verify', { code: '123456' })
    expect(wrapper.find('[data-test="totp-verified"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('1111-2222')
  })

  it('skip navigates to the redirect query target', async () => {
    const router = await makeRouter({ redirect: '/me' })
    const wrapper = mount(SetupLocalSigninView, {
      global: { plugins: [router, makeI18n()] },
    })
    await flushPromises()

    await wrapper.get('[data-test="skip"]').trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.fullPath).toBe('/me')
  })

  it('skip falls back to / when redirect is absent', async () => {
    const router = await makeRouter()
    const wrapper = mount(SetupLocalSigninView, {
      global: { plugins: [router, makeI18n()] },
    })
    await flushPromises()

    await wrapper.get('[data-test="skip"]').trigger('click')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/')
  })

  it('a failing password set shows an error instead of success', async () => {
    post.mockRejectedValue({ code: 'password_weak', message: 'weak' })
    const wrapper = await mountView()

    await wrapper.get('[data-test="choose-password"]').trigger('click')
    await wrapper.get('#setup-pw-new').setValue('short')
    await wrapper.get('#setup-pw-confirm').setValue('short')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    // Client-side minimum length fires before any request.
    expect(post).not.toHaveBeenCalledWith('/api/prohibitorum/me/password/set', expect.anything())

    // Server-side rejection surfaces via the error panel.
    await wrapper.get('#setup-pw-new').setValue('long enough password')
    await wrapper.get('#setup-pw-confirm').setValue('long enough password')
    post.mockRejectedValueOnce({ code: 'server_error', message: 'boom' })
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.find('[data-test="password-done"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('undefined')
  })
})
