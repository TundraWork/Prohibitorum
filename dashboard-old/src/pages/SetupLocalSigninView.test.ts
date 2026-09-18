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

// The sudo ceremony itself is covered in SudoModal.test.ts.
vi.mock('@/components/custom/SudoModal.vue', () => ({
  default: { template: '<div data-test="sudo-modal" />' },
}))

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'

vi.mock('@/lib/webauthn', () => ({
  passkeyGet: vi.fn(),
  passkeyRegister: vi.fn(),
  isUserCancel: () => false,
}))
import { passkeyRegister } from '@/lib/webauthn'

const { hardRedirect } = vi.hoisted(() => ({ hardRedirect: vi.fn() }))
vi.mock('@/lib/navigate', () => ({ hardRedirect }))

vi.mock('@/lib/sudo', () => ({
  withSudo: (fn: () => unknown) => fn(),
  ensureSudo: vi.fn(),
  sudoState: { value: { open: false, resolve: null } },
  _resolveSudo: vi.fn(),
}))

const post = vi.mocked(api.post)
const registerPasskey = vi.mocked(passkeyRegister)

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
  registerPasskey.mockReset()
})

describe('SetupLocalSigninView', () => {
  it('offers a passkey entry and a password+authenticator entry, with no standalone authenticator entry', async () => {
    const wrapper = await mountView()

    expect(wrapper.find('[data-test="choose-passkey"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="choose-password-totp"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="skip"]').exists()).toBe(true)
    // Only the two method entries + skip; authenticator is not a path of its own.
    expect(wrapper.findAll('button')).toHaveLength(3)
  })

  it('registers a passkey and hard-redirects to the redirect query target', async () => {
    post.mockImplementation(async (path: string) => {
      if (path.endsWith('/credentials/register/begin')) return { challenge: 'c' }
      if (path.endsWith('/credentials/register/complete')) return {}
      throw new Error(`unexpected POST ${path}`)
    })
    registerPasskey.mockResolvedValue({ id: 'cred', response: {} } as never)

    const wrapper = await mountView({ redirect: '/me' })
    await wrapper.get('[data-test="choose-passkey"]').trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/credentials/register/begin')
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/credentials/register/complete', { id: 'cred', response: {} })
    expect(hardRedirect).toHaveBeenCalledWith('/me')
  })

  it('sets password + authenticator, shows recovery codes, and only redirects once confirmed', async () => {
    post.mockImplementation(async (path: string) => {
      if (path.endsWith('/password-totp/begin')) return { secret_base32: 'JBSWY3DP', otpauth_uri: 'otpauth://totp/x' }
      if (path.endsWith('/password-totp/verify')) return { recovery_codes: ['1111-2222', '3333-4444'] }
      throw new Error(`unexpected POST ${path}`)
    })
    const wrapper = await mountView({ redirect: '/consent' })

    await wrapper.get('[data-test="choose-password-totp"]').trigger('click')
    await wrapper.get('#setup-pw-new').setValue('correct horse battery staple')
    await wrapper.get('#setup-pw-confirm').setValue('correct horse battery staple')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/password-totp/begin', {
      password: 'correct horse battery staple',
    })

    await wrapper.get('#setup-totp-code').setValue('123456')
    await wrapper.get('[data-test="verify-totp"]').trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/password-totp/verify', { code: '123456' })
    expect(wrapper.text()).toContain('1111-2222')
    // Recovery codes are not yet acknowledged — stay on the page.
    expect(hardRedirect).not.toHaveBeenCalled()

    await wrapper.get('[data-test="saved"]').trigger('click')
    await wrapper.get('[data-test="done"]').trigger('click')
    await flushPromises()

    expect(hardRedirect).toHaveBeenCalledWith('/consent')
  })

  it('returns to the password step when the ceremony stash has expired', async () => {
    post.mockImplementation(async (path: string) => {
      if (path.endsWith('/password-totp/begin')) return { secret_base32: 'JBSWY3DP', otpauth_uri: 'otpauth://totp/x' }
      if (path.endsWith('/password-totp/verify')) throw { code: 'ceremony_expired' }
      throw new Error(`unexpected POST ${path}`)
    })
    const wrapper = await mountView({ redirect: '/consent' })

    await wrapper.get('[data-test="choose-password-totp"]').trigger('click')
    await wrapper.get('#setup-pw-new').setValue('correct horse battery staple')
    await wrapper.get('#setup-pw-confirm').setValue('correct horse battery staple')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('#setup-totp-code').exists()).toBe(true)

    await wrapper.get('#setup-totp-code').setValue('123456')
    await wrapper.get('[data-test="verify-totp"]').trigger('click')
    await flushPromises()

    // The code screen has no back button, so an expired stash must land the
    // user back on a step that can start a fresh ceremony — with the reason
    // still on screen.
    expect(wrapper.find('#setup-pw-new').exists()).toBe(true)
    expect(wrapper.find('[data-test="verify-totp"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="error-summary"]').text()).toBe(en.errors.codes.ceremony_expired)
    expect(hardRedirect).not.toHaveBeenCalled()
  })

  it('does not send a short password, and stays on the password step when begin fails', async () => {
    const wrapper = await mountView()

    await wrapper.get('[data-test="choose-password-totp"]').trigger('click')
    await wrapper.get('#setup-pw-new').setValue('short')
    await wrapper.get('#setup-pw-confirm').setValue('short')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(post).not.toHaveBeenCalled()

    await wrapper.get('#setup-pw-new').setValue('long enough password')
    await wrapper.get('#setup-pw-confirm').setValue('long enough password')
    post.mockRejectedValueOnce({ code: 'server_error', message: 'boom' })
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    // No QR step: the password form is still the visible step.
    expect(wrapper.find('#setup-pw-new').exists()).toBe(true)
    expect(wrapper.find('[data-test="verify-totp"]').exists()).toBe(false)
  })

  it('skip hard-redirects to the redirect query target', async () => {
    const wrapper = await mountView({ redirect: '/me' })
    await wrapper.get('[data-test="skip"]').trigger('click')
    await flushPromises()
    expect(hardRedirect).toHaveBeenCalledWith('/me')
  })

  it('skip falls back to / when redirect is absent', async () => {
    const wrapper = await mountView()
    await wrapper.get('[data-test="skip"]').trigger('click')
    await flushPromises()
    expect(hardRedirect).toHaveBeenCalledWith('/')
  })

  it('rejects an off-origin redirect value', async () => {
    // redirectTarget feeds window.location.assign, so a protocol-relative or
    // backslash-normalised value must never reach hardRedirect.
    for (const evil of ['//evil.com', '/\\evil.com', 'https://evil.com/']) {
      hardRedirect.mockReset()
      const wrapper = await mountView({ redirect: evil })
      await wrapper.get('[data-test="skip"]').trigger('click')
      await flushPromises()
      expect(hardRedirect).toHaveBeenCalledWith('/')
    }
  })
})