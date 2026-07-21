import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createRouter, createMemoryHistory, type Router } from 'vue-router'
import { defineComponent } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import en from '@/locales/en'
import EnrollView from './EnrollView.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'

vi.mock('@/lib/webauthn', () => ({
  passkeyGet: vi.fn(),
  passkeyRegister: vi.fn(async () => ({ id: 'cred', response: {} })),
  isUserCancel: () => false,
}))
import { passkeyRegister } from '@/lib/webauthn'

const { hardRedirect } = vi.hoisted(() => ({ hardRedirect: vi.fn() }))
vi.mock('@/lib/navigate', () => ({ hardRedirect }))

const get = vi.mocked(api.get)
const post = vi.mocked(api.post)
const registerPasskey = vi.mocked(passkeyRegister)

const stub = defineComponent({ template: '<div/>' })
const TOKEN = 'tok_abc'

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

async function makeRouter(): Promise<Router> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/enroll/:token', name: 'enroll', component: EnrollView },
      { path: '/error', name: 'error', component: stub },
    ],
  })
  router.push(`/enroll/${TOKEN}`)
  await router.isReady()
  return router
}

async function mountView(router: Router) {
  const wrapper = mount(EnrollView, { global: { plugins: [router, makeI18n()] } })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  setActivePinia(createPinia())
  get.mockReset()
  post.mockReset()
  registerPasskey.mockClear()
  hardRedirect.mockReset()
})

describe('EnrollView', () => {
  it('invite intent renders the username + displayName fields', async () => {
    get.mockResolvedValue({ intent: 'invite', expiresAt: '2099-01-01T00:00:00Z' })
    const wrapper = await mountView(await makeRouter())

    expect(get).toHaveBeenCalledWith(`/api/prohibitorum/enrollments/${TOKEN}`)
    expect(wrapper.find('input[name=username]').exists()).toBe(true)
    expect(wrapper.find('input[name=displayName]').exists()).toBe(true)
  })

  it('uses shared local-account enrollment after VRChat verification', async () => {
    get.mockResolvedValue({
      intent: 'federated_register',
      suggestedDisplayName: 'VRChat Friend',
      expiresAt: '2099-01-01T00:00:00Z',
    })
    post.mockResolvedValue({ challenge: 'c' })
    const wrapper = await mountView(await makeRouter())

    expect(wrapper.findAll('h1')).toHaveLength(1)
    expect(wrapper.get('h1').text()).toBe('Set up your local account')
    expect(wrapper.get('[data-test="federated-register-intro"]').text()).toBe(
      'Your VRChat profile is verified. Choose your local account details, then create a passkey to sign in here.',
    )

    const username = wrapper.get('input[name="username"]')
    const displayName = wrapper.get('input[name="displayName"]')
    expect(wrapper.get('label[for="enroll-username"]').exists()).toBe(true)
    expect(wrapper.get('label[for="enroll-displayname"]').exists()).toBe(true)
    expect(username.element.getAttribute('required')).not.toBeNull()
    expect((username.element as HTMLInputElement).value).toBe('')
    expect((displayName.element as HTMLInputElement).value).toBe('VRChat Friend')

    await username.setValue('local_friend')
    await displayName.setValue('Edited Display Name')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(
      `/api/prohibitorum/enrollments/${TOKEN}/register/begin`,
      { username: 'local_friend', displayName: 'Edited Display Name' },
    )
  })

  it('reset intent shows the read-only target, no identity fields', async () => {
    get.mockResolvedValue({
      intent: 'reset',
      target: { username: 'alex', displayName: 'Alex' },
      expiresAt: '2099-01-01T00:00:00Z',
    })
    const wrapper = await mountView(await makeRouter())

    expect(wrapper.find('input[name=username]').exists()).toBe(false)
    expect(wrapper.text()).toContain('alex')
  })

  it('renders provider-backed recovery without exposing or submitting an identity', async () => {
    get.mockResolvedValue({
      intent: 'reset',
      expiresAt: '2099-01-01T00:00:00Z',
    })
    post.mockResolvedValue({ challenge: 'c' })
    const wrapper = await mountView(await makeRouter())

    expect(wrapper.findAll('h1')).toHaveLength(1)
    expect(wrapper.get('h1').text()).toBe('Recover access')
    expect(wrapper.get('[data-test="recovery-intro"]').text()).toBe(
      'Create a new passkey to recover access to your account.',
    )
    expect(wrapper.find('input[name="username"]').exists()).toBe(false)
    expect(wrapper.find('input[name="displayName"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="target-account"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('undefined')

    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(
      `/api/prohibitorum/enrollments/${TOKEN}/register/begin`,
      undefined,
    )
  })

  it('registers a passkey and auto-logs-in to the app root', async () => {
    get.mockResolvedValue({ intent: 'invite', expiresAt: '2099-01-01T00:00:00Z' })
    post.mockImplementation(async (path: string) => {
      if (path.endsWith('/register/begin')) return { challenge: 'c' }
      if (path.endsWith('/register/complete')) return { session: { id: 1 }, newCredentialId: 9 }
      throw new Error(`unexpected POST ${path}`)
    })
    const wrapper = await mountView(await makeRouter())

    await wrapper.find('input[name=username]').setValue('alex')
    await wrapper.find('input[name=displayName]').setValue('Alex Smith')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(
      `/api/prohibitorum/enrollments/${TOKEN}/register/begin`,
      { username: 'alex', displayName: 'Alex Smith' },
    )
    expect(post).toHaveBeenCalledWith(
      `/api/prohibitorum/enrollments/${TOKEN}/register/complete`,
      expect.objectContaining({ id: 'cred' }),
    )
    expect(hardRedirect).toHaveBeenCalledWith('/')
  })

  it('shows federation interstitial when begin returns enrollment_federation_required', async () => {
    get.mockResolvedValue({ intent: 'invite', expiresAt: '2099-01-01T00:00:00Z' })
    post.mockRejectedValue({ code: 'enrollment_federation_required', message: '须联合注册' })
    const wrapper = await mountView(await makeRouter())

    await wrapper.find('input[name=username]').setValue('alex')
    await wrapper.find('input[name=displayName]').setValue('Alex Smith')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    // Must NOT redirect immediately — show the interstitial instead.
    expect(hardRedirect).not.toHaveBeenCalled()
    // The interstitial continue button should be present.
    const continueBtn = wrapper.find('[data-test="federation-continue"]')
    expect(continueBtn.exists()).toBe(true)
    // The form should no longer be visible.
    expect(wrapper.find('input[name=username]').exists()).toBe(false)
    // The passkey complete must NOT have been attempted.
    expect(post).not.toHaveBeenCalledWith(
      expect.stringContaining('/register/complete'),
      expect.anything(),
    )
  })

  it('continues to the federation URL when the interstitial button is clicked', async () => {
    get.mockResolvedValue({ intent: 'invite', expiresAt: '2099-01-01T00:00:00Z' })
    post.mockRejectedValue({ code: 'enrollment_federation_required', message: '须联合注册' })
    const wrapper = await mountView(await makeRouter())

    await wrapper.find('input[name=username]').setValue('alex')
    await wrapper.find('input[name=displayName]').setValue('Alex Smith')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    await wrapper.find('[data-test="federation-continue"]').trigger('click')
    await flushPromises()

    expect(hardRedirect).toHaveBeenCalledWith(
      `/api/prohibitorum/enrollments/${TOKEN}/start-federation?return_to=${encodeURIComponent('/')}`,
    )
  })

  it('reset target renders as plain mono text, not a CodeField', async () => {
    get.mockResolvedValue({
      intent: 'reset',
      target: { username: 'alex', displayName: 'Alex' },
      expiresAt: '2099-01-01T00:00:00Z',
    })
    const wrapper = await mountView(await makeRouter())

    // Username shown as plain text
    expect(wrapper.text()).toContain('alex')
    // No copy button (CodeField would have one)
    const copyBtn = wrapper.findAll('button').find((b) => b.text().includes('Copy') || b.attributes('aria-label')?.includes('copy'))
    expect(copyBtn).toBeUndefined()
  })

  it('shows passkey foreshadow line on the enrollment form', async () => {
    get.mockResolvedValue({ intent: 'invite', expiresAt: '2099-01-01T00:00:00Z' })
    const wrapper = await mountView(await makeRouter())

    expect(wrapper.text()).toContain('your device will ask you to create a passkey')
  })

  it('clears a displayed enrollment error when dismissed', async () => {
    get.mockResolvedValue({ intent: 'invite', expiresAt: '2099-01-01T00:00:00Z' })
    post.mockRejectedValue({ code: 'forbidden' })
    const wrapper = await mountView(await makeRouter())

    await wrapper.find('input[name=username]').setValue('alex')
    await wrapper.find('input[name=displayName]').setValue('Alex Smith')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)

    await wrapper.get('[data-test="error-dismiss"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('clears a displayed WebAuthn error when dismissed', async () => {
    get.mockResolvedValue({ intent: 'invite', expiresAt: '2099-01-01T00:00:00Z' })
    post.mockResolvedValue({ challenge: 'c' })
    registerPasskey.mockRejectedValueOnce(new Error('browser failure'))
    const wrapper = await mountView(await makeRouter())

    await wrapper.find('input[name=username]').setValue('alex')
    await wrapper.find('input[name=displayName]').setValue('Alex Smith')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain(en.errors.codes.webauthn_error)

    await wrapper.get('[data-test="error-dismiss"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('does not render provider snapshot data from an enrollment preview', async () => {
    get.mockResolvedValue({
      intent: 'federated_register',
      suggestedDisplayName: 'Visible suggestion',
      providerSlug: 'private-provider',
      subject: 'private-subject',
      metadata: { secret: 'private-metadata' },
      avatarUrl: 'https://private.example/avatar',
      expiresAt: '2099-01-01T00:00:00Z',
    })
    const wrapper = await mountView(await makeRouter())

    expect((wrapper.get('input[name="displayName"]').element as HTMLInputElement).value).toBe(
      'Visible suggestion',
    )
    const html = wrapper.html()
    expect(html).not.toContain('private-provider')
    expect(html).not.toContain('private-subject')
    expect(html).not.toContain('private-metadata')
    expect(html).not.toContain('https://private.example/avatar')
  })

  it('offers the password+TOTP method when allowedMethods includes it', async () => {
    get.mockResolvedValue({
      intent: 'invite',
      expiresAt: '2099-01-01T00:00:00Z',
      allowedMethods: ['passkey', 'password_totp'],
    })
    const wrapper = await mountView(await makeRouter())

    // Passkey remains the primary submit; password+TOTP is the secondary option.
    expect(wrapper.get('button[type="submit"]').text()).toBe(en.enroll.registerButton)
    const chooser = wrapper.get('[data-test="choose-password-totp"]')
    expect(chooser.text()).toBe(en.enroll.methodPasswordTotp)

    // Fill identity, then switch to the password+TOTP ceremony.
    await wrapper.get('input[name=username]').setValue('alex')
    await wrapper.get('input[name=displayName]').setValue('Alex Smith')
    await chooser.trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="enroll-password-totp"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="pwtotp-continue"]').exists()).toBe(true)
    // Identity fields lock while the ceremony owns the pending account.
    expect((wrapper.get('input[name=username]').element as HTMLInputElement).disabled).toBe(true)
  })

  it('hides the password+TOTP option for a passkey-only (bootstrap) enrollment', async () => {
    get.mockResolvedValue({
      intent: 'bootstrap',
      expiresAt: '2099-01-01T00:00:00Z',
      allowedMethods: ['passkey'],
    })
    const wrapper = await mountView(await makeRouter())

    expect(wrapper.find('[data-test="choose-password-totp"]').exists()).toBe(false)
    expect(wrapper.get('button[type="submit"]').text()).toBe(en.enroll.registerButton)
  })

  it('runs the password+TOTP ceremony: begin→verify→recovery codes→app root', async () => {
    get.mockResolvedValue({
      intent: 'invite',
      expiresAt: '2099-01-01T00:00:00Z',
      allowedMethods: ['passkey', 'password_totp'],
    })
    post.mockImplementation(async (path: string) => {
      if (path.endsWith('/password-totp/begin')) {
        return { secret_base32: 'ABCDEF', otpauth_uri: 'otpauth://totp/x?secret=ABCDEF' }
      }
      if (path.endsWith('/password-totp/verify')) return { session: { id: 1 }, recoveryCodes: Array(10).fill('code') }
      throw new Error(`unexpected POST ${path}`)
    })
    const wrapper = await mountView(await makeRouter())

    await wrapper.get('input[name=username]').setValue('alex')
    await wrapper.get('input[name=displayName]').setValue('Alex Smith')
    await wrapper.get('[data-test="choose-password-totp"]').trigger('click')
    await flushPromises()

    await wrapper.get('#enroll-password').setValue('supersecret')
    await wrapper.get('#enroll-password-confirm').setValue('supersecret')
    await wrapper.get('[data-test="pwtotp-continue"]').trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(
      `/api/prohibitorum/enrollments/${TOKEN}/password-totp/begin`,
      { password: 'supersecret', username: 'alex', displayName: 'Alex Smith' },
    )

    await wrapper.get('#enroll-totp-code').setValue('123456')
    await wrapper.get('[data-test="pwtotp-verify"]').trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(
      `/api/prohibitorum/enrollments/${TOKEN}/password-totp/verify`,
      { code: '123456' },
    )
    // The recovery codes are shown (RecoveryCodesDisplay renders its saved-gate).
    expect(wrapper.find('[data-test="done"]').exists()).toBe(true)
  })

  it.each(['enrollment_invalid', 'enrollment_expired', 'enrollment_consumed'])(
    'routes the %s preview error through the existing code-based error page',
    async (code) => {
      get.mockRejectedValue({ code })
      const router = await makeRouter()
      await mountView(router)

      expect(router.currentRoute.value.name).toBe('error')
      expect(router.currentRoute.value.query.error).toBe(code)
    },
  )
})
