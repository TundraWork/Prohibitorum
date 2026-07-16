import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import zh from '@/locales/zh'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const { withSudo, push, routeParams } = vi.hoisted(() => ({
  withSudo: vi.fn((fn: () => Promise<unknown>) => fn()),
  push: vi.fn(),
  routeParams: { slug: 'okta' },
}))
vi.mock('@/lib/sudo', () => ({ withSudo }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post); const put = vi.mocked(api.put)
vi.mock('vue-router', () => ({ useRoute: () => ({ params: routeParams }), useRouter: () => ({ push }), RouterLink: { template: '<a><slot/></a>' } }))
import AdminUpstreamIdpDetailView from './AdminUpstreamIdpDetailView.vue'

const OIDC_CONFIG = {
  issuerUrl: 'https://okta/',
  clientId: 'c1',
  scopes: ['openid', 'email'],
  allowedDomains: ['ex.com'],
  usernameClaim: 'preferred_username',
  displayNameClaim: 'name',
  emailClaim: 'email',
  pictureClaim: 'picture',
  requireVerifiedEmail: false,
  allowPrivateNetwork: false,
}
const IDP = {
  slug: 'okta',
  displayName: 'Okta',
  protocol: 'oidc',
  mode: 'auto_provision',
  config: OIDC_CONFIG,
  disabled: false,
  secretConfigured: true,
  secretStatus: 'valid',
  secretValidatedAt: null,
  ready: true,
  supportsOperator: false,
  searchFields: [],
  createdAt: '2026-01-01T00:00:00Z',
}
const VRCHAT = {
  ...IDP,
  slug: 'vrchat',
  displayName: 'VRChat moderation',
  protocol: 'vrchat',
  config: {},
  disabled: true,
  secretConfigured: false,
  secretStatus: 'unconfigured',
  secretValidatedAt: null,
  ready: false,
  supportsOperator: true,
}
const i18n = (locale = 'en') => createI18n({ legacy: false, locale, fallbackLocale: 'en', messages: { en, zh } })
const mountView = (locale = 'en') => mount(AdminUpstreamIdpDetailView, { global: { plugins: [i18n(locale)], stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } } }, attachTo: document.body })
const mountVrchat = async (overrides: Record<string, unknown> = {}) => {
  routeParams.slug = 'vrchat'
  get.mockResolvedValue({ ...VRCHAT, ...overrides })
  const wrapper = mountView()
  await flushPromises()
  return wrapper
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}
const clickConfirm = async (label: string) => {
  const btns = Array.from(document.body.querySelectorAll('button')).filter((b) => b.textContent?.trim() === label && b.classList.contains('bg-destructive'))
  btns[btns.length - 1]!.click(); await flushPromises()
}
beforeEach(() => {
  get.mockReset(); post.mockReset(); put.mockReset(); push.mockReset(); withSudo.mockClear()
  routeParams.slug = 'okta'
})

describe('AdminUpstreamIdpDetailView', () => {
  it('shows not-found on upstream_idp_not_found', async () => {
    get.mockRejectedValue({ code: 'upstream_idp_not_found', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.upstream.notFound)
  })
  it('shows the slug as a read-only field (not an editable input)', async () => {
    get.mockResolvedValue(IDP)
    const w = mountView(); await flushPromises()
    // The read-only slug element exists and shows the slug value
    const slugEl = w.find('[data-test="idp-slug"]')
    expect(slugEl.exists()).toBe(true)
    expect(slugEl.text()).toBe('okta')
    // No input has name="slug" (slug is the immutable identifier, not editable)
    expect(w.find('input[name="slug"]').exists()).toBe(false)
  })
  it('saves adapter config via PUT without disabled state or secret', async () => {
    get.mockResolvedValue(IDP); put.mockResolvedValue({ ...IDP, displayName: 'Okta 2' })
    const w = mountView(); await flushPromises()
    await w.find('input[name="displayName"]').setValue('Okta 2')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/okta', {
      displayName: 'Okta 2',
      mode: 'auto_provision',
      config: OIDC_CONFIG,
    })
    const body = put.mock.calls[0][1] as Record<string, unknown>
    expect(body).not.toHaveProperty('disabled')
    expect(body).not.toHaveProperty('secret')
    expect(body).not.toHaveProperty('clientSecret')
    expect(w.text()).toContain(en.admin.upstream.saved)
  })
  it('groups the disable button, rotate-secret and delete in the Danger zone card', async () => {
    get.mockResolvedValue(IDP)
    const w = mountView(); await flushPromises()
    const cards = w.findAll('[data-slot="card"]')
    // The Danger zone is the card holding Delete; it now also holds disable + rotate.
    const dangerCard = cards.find((c) => c.find('[data-test="delete"]').exists())
    expect(dangerCard).toBeTruthy()
    expect(dangerCard!.find('[data-test="disable-toggle"]').exists()).toBe(true)
    expect(dangerCard!.find('[data-test="rotate"]').exists()).toBe(true)
    // The disable control must NOT be in the Config card (the one holding Save).
    const configCard = cards.find((c) => c.find('[data-test="save"]').exists())
    expect(configCard!.find('[data-test="disable-toggle"]').exists()).toBe(false)
  })
  it('disables independently via the dedicated set-disabled endpoint (no config PUT)', async () => {
    get.mockResolvedValue(IDP) // disabled: false → button reads "Disable"
    post.mockResolvedValue({ ...IDP, disabled: true })
    const w = mountView(); await flushPromises()
    const btn = w.find('[data-test="disable-toggle"]')
    expect(btn.text()).toBe(en.admin.upstream.disable)
    expect(w.find('[data-test="status-badge"]').text()).toBe(en.admin.upstream.active) // green "Active"
    await btn.trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/set-disabled', { slug: 'okta', disabled: true })
    expect(put).not.toHaveBeenCalled() // independent of the config Save
    expect(w.find('[data-test="disable-toggle"]').text()).toBe(en.admin.upstream.enable) // flipped to "Enable"
    expect(w.find('[data-test="status-badge"]').text()).toBe(en.admin.upstream.disabled) // now "Disabled"
  })
  it('includes pictureClaim in save payload and renders the input', async () => {
    get.mockResolvedValue(IDP); put.mockResolvedValue({ ...IDP, config: { ...OIDC_CONFIG, pictureClaim: 'avatar_url' } })
    const w = mountView(); await flushPromises()
    expect(w.find('input[name="pictureClaim"]').exists()).toBe(true)
    await w.find('input[name="pictureClaim"]').setValue('avatar_url')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/okta', expect.objectContaining({
      config: expect.objectContaining({ pictureClaim: 'avatar_url' }),
    }))
  })
  it('renders claim inputs as a compact grid with default-value placeholders', async () => {
    get.mockResolvedValue(IDP)
    const w = mountView(); await flushPromises()
    expect(w.find('[data-test="claim-username"]').attributes('placeholder')).toBe('preferred_username')
    expect(w.find('[data-test="claim-displayName"]').attributes('placeholder')).toBe('name')
    expect(w.find('[data-test="claim-email"]').attributes('placeholder')).toBe('email')
    expect(w.find('[data-test="claim-avatar"]').attributes('placeholder')).toBe('picture')
    // All four inputs still bound to correct v-models (values round-trip from IDP fixture)
    expect((w.find('input[name="usernameClaim"]').element as HTMLInputElement).value).toBe('preferred_username')
    expect((w.find('input[name="emailClaim"]').element as HTMLInputElement).value).toBe('email')
  })
  it('rotates the secret without revealing a value', async () => {
    get.mockResolvedValue(IDP); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('input[name="newSecret"]').setValue('newsek')
    await w.find('[data-test="rotate"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/rotate-secret', { slug: 'okta', secret: 'newsek' })
    expect(w.text()).toContain(en.admin.upstream.rotated)
  })
  it('deletes and navigates back to the list', async () => {
    get.mockResolvedValue(IDP); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    await clickConfirm(en.admin.upstream.delete)
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/delete', { slug: 'okta' })
    expect(push).toHaveBeenCalledWith('/admin/identity-providers')
  })
  it('surfaces a generic app load error in the Alert without showing not-found', async () => {
    // A non-not-found app 4xx still renders inline; connectivity/5xx
    // (server_error) is now suppressed here and shown via the global toast.
    get.mockRejectedValue({ code: 'forbidden', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.errors.codes.forbidden)
    expect(w.text()).not.toContain(en.admin.upstream.notFound)
  })
  it('does not navigate when delete fails', async () => {
    get.mockResolvedValue(IDP); post.mockRejectedValue({ code: 'server_error', message: 'boom' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    await clickConfirm(en.admin.upstream.delete)
    expect(push).not.toHaveBeenCalled()
  })
  it('renders the private network toggle with a security warning', async () => {
    get.mockResolvedValue(IDP)
    const w = mountView(); await flushPromises()
    expect(w.find('[data-test="allowPrivateNetwork"]').exists()).toBe(true)
    const warning = w.get('[data-test="private-network-warning"]')
    expect(warning.attributes('data-slot')).toBe('alert')
    expect(warning.get('[data-slot="alert-description"]').text()).toContain(
      en.admin.upstream.allowPrivateNetworkWarning,
    )
  })
  it('includes allowPrivateNetwork in the save payload', async () => {
    get.mockResolvedValue({ ...IDP, allowPrivateNetwork: false })
    put.mockResolvedValue({ ...IDP, allowPrivateNetwork: true })
    const w = mountView(); await flushPromises()
    // Toggle the switch on
    await w.find('[data-test="allowPrivateNetwork"]').trigger('click')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    const body = put.mock.calls[0][1] as Record<string, unknown>
    expect(body).toHaveProperty('config')
    expect(body.config).toEqual(expect.objectContaining({ allowPrivateNetwork: true }))
  })
  it('saves Steam with an exact empty adapter config and no OIDC controls', async () => {
    const steam = { ...IDP, slug: 'steam', displayName: 'Steam', protocol: 'steam', config: {}, secretStatus: 'configured' }
    get.mockResolvedValue(steam)
    put.mockResolvedValue(steam)
    const w = mountView(); await flushPromises()
    expect(w.find('input[name="issuerUrl"]').exists()).toBe(false)
    expect(w.find('input[name="allowedDomains"]').exists()).toBe(false)
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/okta', {
      displayName: 'Steam',
      mode: 'auto_provision',
      config: {},
    })
  })
  it('shows VRChat session state, setup guidance, and no generic secret UI', async () => {
    const w = await mountVrchat()

    expect(w.find('input[name="issuerUrl"]').exists()).toBe(false)
    expect(w.find('input[name="usernameClaim"]').exists()).toBe(false)
    expect(w.find('[data-test="allowPrivateNetwork"]').exists()).toBe(false)
    expect(w.find('input[name="newSecret"]').exists()).toBe(false)
    expect(w.find('[data-test="rotate"]').exists()).toBe(false)
    expect(w.get('[data-test="operator-status-badge"]').text()).toBe(en.admin.upstream.operatorStatusUnconfigured)
    expect(w.text()).toContain(en.admin.upstream.operatorLastValidationNever)
    expect(w.text()).toContain(en.admin.upstream.operatorSessionNotice)
    const riskWarning = w.get('[data-test="operator-risk-warning"]')
    expect(riskWarning.text()).toContain(en.admin.upstream.vrchatCreateWarning)
    expect(riskWarning.get('[data-slot="alert-description"]').classes()).toContain('max-w-[75ch]')
    expect(w.get('[data-test="operator-session-notice"]').get('[data-slot="alert-description"]').classes()).toContain('max-w-[75ch]')
    expect(riskWarning.attributes('role')).toBe('note')
    expect(w.get('[data-test="operator-session-notice"]').attributes('role')).toBe('note')
    const liveRegion = w.get('[data-test="operator-live-region"]')
    expect(liveRegion.attributes('role')).toBe('status')
    expect(liveRegion.attributes('aria-live')).toBe('polite')
    expect(w.get('label[for="operatorUsername"]').text()).toBe(en.admin.upstream.operatorUsername)
    expect(w.get('label[for="operatorPassword"]').text()).toBe(en.admin.upstream.operatorPassword)
    expect(w.get('[data-test="operator-credentials-form"]').element.tagName).toBe('FORM')
    expect(w.find('input[name="operatorUsername"]').exists()).toBe(true)
    expect(w.find('input[name="operatorPassword"]').exists()).toBe(true)
    expect(w.get('[data-test="disable-toggle"]').attributes('disabled')).toBeDefined()
    expect(w.text()).toContain(en.admin.upstream.operatorEnableRequiresValid)
  })

  it('renders the unofficial API and session risk warning in Chinese', async () => {
    routeParams.slug = 'vrchat'
    get.mockResolvedValue(VRCHAT)
    const w = mountView('zh')
    await flushPromises()

    expect(w.get('[data-test="operator-risk-warning"]').text()).toContain(
      zh.admin.upstream.vrchatCreateWarning,
    )
    expect(w.get('[data-test="operator-session-notice"]').text()).toContain(
      zh.admin.upstream.operatorSessionNotice,
    )
  })

  it('shows invalid status and returns directly to credential setup', async () => {
    const w = await mountVrchat({
      secretConfigured: true,
      secretStatus: 'invalid',
      secretValidatedAt: '2026-07-15T08:00:00Z',
    })

    expect(w.get('[data-test="operator-status-badge"]').text()).toBe(en.admin.upstream.operatorStatusInvalid)
    expect(w.find('input[name="operatorUsername"]').exists()).toBe(true)
    expect(w.find('input[name="operatorPassword"]').exists()).toBe(true)
    expect(w.find('[data-test="operator-validate"]').exists()).toBe(false)
  })

  it('shows valid status, validation time, and validate/replace actions', async () => {
    const w = await mountVrchat({
      disabled: true,
      secretConfigured: true,
      secretStatus: 'valid',
      secretValidatedAt: '2026-07-16T10:30:00Z',
      ready: true,
    })

    expect(w.get('[data-test="operator-status-badge"]').text()).toBe(en.admin.upstream.operatorStatusValid)
    expect(w.get('[data-test="operator-last-validation"]').text()).toContain('2026')
    expect(w.find('[data-test="operator-validate"]').exists()).toBe(true)
    expect(w.find('[data-test="operator-replace"]').exists()).toBe(true)
    expect(w.find('input[name="operatorUsername"]').exists()).toBe(false)
    expect(w.get('[data-test="disable-toggle"]').attributes('disabled')).toBeUndefined()
  })

  it('formats validation time with the active locale', async () => {
    routeParams.slug = 'vrchat'
    const validatedAt = '2026-07-16T10:30:00Z'
    get.mockResolvedValue({
      ...VRCHAT,
      secretConfigured: true,
      secretStatus: 'valid',
      secretValidatedAt: validatedAt,
      ready: true,
    })
    const w = mountView('zh')
    await flushPromises()

    expect(w.get('[data-test="operator-last-validation"]').text()).toContain(
      new Date(validatedAt).toLocaleString('zh'),
    )
  })

  it('starts a progressive challenge with supported returned methods only', async () => {
    post.mockResolvedValue({
      status: 'challenge',
      challenge: 'opaque-challenge-value',
      methods: ['emailOtp', 'unsupported', 'totp', 'otp'],
      expiresAt: '2026-07-17T12:10:00Z',
    })
    const w = await mountVrchat()
    await w.get('input[name="operatorUsername"]').setValue('operator@example.com')
    await w.get('input[name="operatorPassword"]').setValue('not-rendered-password')
    await w.get('[data-test="operator-start"]').trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(
      '/api/prohibitorum/identity-providers/vrchat/operator-session/start',
      { username: 'operator@example.com', password: 'not-rendered-password' },
    )
    expect(withSudo).toHaveBeenCalledWith(expect.any(Function), en.sudo.reason.startOperatorSession)
    expect(w.find('input[name="operatorUsername"]').exists()).toBe(false)
    expect(w.find('input[name="operatorPassword"]').exists()).toBe(false)
    expect(w.find('[data-test="radio-card-emailOtp"]').exists()).toBe(true)
    expect(w.find('[data-test="radio-card-totp"]').exists()).toBe(true)
    expect(w.find('[data-test="radio-card-otp"]').exists()).toBe(true)
    expect(w.get('[data-test="radio-card-otp"]').text()).toContain(en.admin.upstream.operatorMethod.otp)
    expect(w.text()).not.toContain('unsupported')
    expect(w.text()).not.toContain('opaque-challenge-value')
    expect(w.get('[data-test="operator-code-form"]').element.tagName).toBe('FORM')
    expect(w.get('label[for="operatorCode"]').text()).toBe(en.admin.upstream.operatorCode)
    expect(w.get('input[name="operatorCode"]').attributes('autocomplete')).toBe('one-time-code')
    expect(document.activeElement).toBe(w.get('input[name="operatorCode"]').element)
  })

  it('rejects malformed operator responses before changing setup state', async () => {
    post.mockResolvedValue({
      status: 'challenge',
      methods: ['totp'],
      expiresAt: '2026-07-17T12:10:00Z',
    })
    const w = await mountVrchat()
    await w.get('input[name="operatorUsername"]').setValue('operator')
    await w.get('input[name="operatorPassword"]').setValue('password')
    await w.get('[data-test="operator-start"]').trigger('click')
    await flushPromises()

    expect(w.find('input[name="operatorUsername"]').exists()).toBe(true)
    expect(w.find('input[name="operatorCode"]').exists()).toBe(false)
    expect(w.text()).toContain(en.errors.unknown)
  })

  it('accepts a no-2FA start result through native form submission', async () => {
    post.mockResolvedValue({
      status: 'valid',
      provider: {
        ...VRCHAT,
        secretConfigured: true,
        secretStatus: 'valid',
        secretValidatedAt: '2026-07-17T12:06:00Z',
        ready: true,
      },
    })
    const w = await mountVrchat()
    await w.get('input[name="operatorUsername"]').setValue('operator')
    await w.get('input[name="operatorPassword"]').setValue('not-retained')
    await w.get('[data-test="operator-credentials-form"]').trigger('submit')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(
      '/api/prohibitorum/identity-providers/vrchat/operator-session/start',
      { username: 'operator', password: 'not-retained' },
    )
    expect(w.get('[data-test="operator-status-badge"]').text()).toBe(en.admin.upstream.operatorStatusValid)
    expect(w.get('[data-test="operator-last-validation"]').text()).toContain('2026')
    expect(w.find('[data-test="operator-validate"]').exists()).toBe(true)
    expect(w.find('[data-test="operator-replace"]').exists()).toBe(true)
    expect(w.find('input[name="operatorUsername"]').exists()).toBe(false)
    expect(w.find('input[name="operatorPassword"]').exists()).toBe(false)
    expect(w.text()).not.toContain('not-retained')
    expect(document.activeElement).toBe(w.get('[data-test="operator-live-region"]').element)
  })

  it('clears a rejected password but keeps the username for retry', async () => {
    post.mockRejectedValue({ code: 'vrchat_operator_credentials_invalid' })
    const w = await mountVrchat()
    await w.get('input[name="operatorUsername"]').setValue('operator@example.com')
    await w.get('input[name="operatorPassword"]').setValue('bad-password')
    await w.get('[data-test="operator-start"]').trigger('click')
    await flushPromises()

    expect((w.get('input[name="operatorUsername"]').element as HTMLInputElement).value).toBe('operator@example.com')
    expect((w.get('input[name="operatorPassword"]').element as HTMLInputElement).value).toBe('')
    expect(w.text()).toContain(en.errors.codes.vrchat_operator_credentials_invalid)
    expect(w.text()).not.toContain('bad-password')
  })

  it.each([
    ['vrchat_operator_code_invalid', 'vrchat_operator_code_invalid'],
    ['upstream_rate_limited', 'upstream_rate_limited'],
    ['upstream_temporarily_unavailable', 'upstream_temporarily_unavailable'],
  ])('keeps the challenge after %s and clears each code attempt', async (errorCode, localeKey) => {
    post
      .mockResolvedValueOnce({
        status: 'challenge',
        challenge: 'retryable-challenge',
        methods: ['totp'],
        expiresAt: '2026-07-17T12:10:00Z',
      })
      .mockRejectedValueOnce({ code: errorCode })
      .mockResolvedValueOnce({ status: 'valid', provider: { ...VRCHAT, secretConfigured: true, secretStatus: 'valid', ready: true } })
    const w = await mountVrchat()
    await w.get('input[name="operatorUsername"]').setValue('operator')
    await w.get('input[name="operatorPassword"]').setValue('password')
    await w.get('[data-test="operator-start"]').trigger('click')
    await flushPromises()
    await w.get('input[name="operatorCode"]').setValue('123456')
    await w.get('[data-test="operator-verify"]').trigger('click')
    await flushPromises()

    expect((w.get('input[name="operatorCode"]').element as HTMLInputElement).value).toBe('')
    expect(w.text()).toContain(en.errors.codes[localeKey as keyof typeof en.errors.codes])
    await w.get('input[name="operatorCode"]').setValue('654321')
    await w.get('[data-test="operator-verify"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenLastCalledWith(
      '/api/prohibitorum/identity-providers/vrchat/operator-session/verify',
      { challenge: 'retryable-challenge', method: 'totp', code: '654321' },
    )
  })

  it('snapshots verification fields before delayed sudo and ignores later edits', async () => {
    post.mockResolvedValueOnce({
      status: 'challenge',
      challenge: 'snapshot-challenge',
      methods: ['totp', 'emailOtp'],
      expiresAt: '2026-07-17T12:10:00Z',
    })
    const w = await mountVrchat()
    await w.get('input[name="operatorUsername"]').setValue('operator')
    await w.get('input[name="operatorPassword"]').setValue('password')
    await w.get('[data-test="operator-start"]').trigger('click')
    await flushPromises()

    const gate = deferred<void>()
    let sudoCallback!: () => Promise<unknown>
    withSudo.mockImplementationOnce(async (callback) => {
      sudoCallback = callback
      await gate.promise
      return callback()
    })
    await w.get('input[name="operatorCode"]').setValue('first-code')
    await w.get('[data-test="operator-verify"]').trigger('click')
    await w.get('input[name="operatorCode"]').setValue('mutated-code')
    await w.get('[data-test="radio-card-emailOtp"]').trigger('click')
    gate.resolve()
    await flushPromises()

    expect(sudoCallback).toBeTypeOf('function')
    expect(post).toHaveBeenLastCalledWith(
      '/api/prohibitorum/identity-providers/vrchat/operator-session/verify',
      { challenge: 'snapshot-challenge', method: 'totp', code: 'first-code' },
    )
  })

  it('resets an expired challenge to credentials and clears the code', async () => {
    post
      .mockResolvedValueOnce({
        status: 'challenge',
        challenge: 'expired-challenge',
        methods: ['otp'],
        expiresAt: '2026-07-17T12:10:00Z',
      })
      .mockRejectedValueOnce({ code: 'vrchat_operator_challenge_invalid' })
    const w = await mountVrchat()
    await w.get('input[name="operatorUsername"]').setValue('operator')
    await w.get('input[name="operatorPassword"]').setValue('password')
    await w.get('[data-test="operator-start"]').trigger('click')
    await flushPromises()
    await w.get('input[name="operatorCode"]').setValue('one-use-code')
    await w.get('[data-test="operator-verify"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenLastCalledWith(
      '/api/prohibitorum/identity-providers/vrchat/operator-session/verify',
      { challenge: 'expired-challenge', method: 'otp', code: 'one-use-code' },
    )

    expect(w.find('input[name="operatorCode"]').exists()).toBe(false)
    expect(w.find('input[name="operatorUsername"]').exists()).toBe(true)
    expect(w.find('input[name="operatorPassword"]').exists()).toBe(true)
    expect(w.text()).toContain(en.errors.codes.vrchat_operator_challenge_invalid)
    expect(w.text()).not.toContain('expired-challenge')
    expect(w.text()).not.toContain('one-use-code')
    expect(document.activeElement).toBe(w.get('input[name="operatorUsername"]').element)
  })

  it('updates the local provider after successful code verification', async () => {
    post
      .mockResolvedValueOnce({
        status: 'challenge',
        challenge: 'challenge',
        methods: ['emailOtp'],
        expiresAt: '2026-07-17T12:10:00Z',
      })
      .mockResolvedValueOnce({
        status: 'valid',
        provider: {
          ...VRCHAT,
          secretConfigured: true,
          secretStatus: 'valid',
          secretValidatedAt: '2026-07-17T12:04:00Z',
          ready: true,
        },
      })
    const w = await mountVrchat()
    await w.get('input[name="operatorUsername"]').setValue('operator')
    await w.get('input[name="operatorPassword"]').setValue('password')
    await w.get('[data-test="operator-start"]').trigger('click')
    await flushPromises()
    await w.get('input[name="operatorCode"]').setValue('112233')
    await w.get('[data-test="operator-verify"]').trigger('click')
    await flushPromises()

    expect(withSudo).toHaveBeenLastCalledWith(expect.any(Function), en.sudo.reason.verifyOperatorSession)
    expect(w.get('[data-test="operator-status-badge"]').text()).toBe(en.admin.upstream.operatorStatusValid)
    expect(w.find('input[name="operatorCode"]').exists()).toBe(false)
    expect(w.find('[data-test="operator-validate"]').exists()).toBe(true)
    expect(document.activeElement).toBe(w.get('[data-test="operator-live-region"]').element)
  })

  it('validates without a body and replaces the session inline', async () => {
    post.mockResolvedValue({
      status: 'valid',
      provider: {
        ...VRCHAT,
        secretConfigured: true,
        secretStatus: 'valid',
        secretValidatedAt: '2026-07-17T12:04:00Z',
        ready: true,
      },
    })
    const w = await mountVrchat({ secretConfigured: true, secretStatus: 'valid', ready: true })
    await w.get('[data-test="operator-validate"]').trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/vrchat/operator-session/validate')
    expect(withSudo).toHaveBeenCalledWith(expect.any(Function), en.sudo.reason.validateOperatorSession)

    await w.get('[data-test="operator-replace"]').trigger('click')
    expect(w.find('input[name="operatorUsername"]').exists()).toBe(true)
    expect(w.find('input[name="operatorPassword"]').exists()).toBe(true)
    expect(w.find('[data-test="operator-validate"]').exists()).toBe(false)
    await flushPromises()
    expect(document.activeElement).toBe(w.get('input[name="operatorUsername"]').element)
  })

  it('refreshes invalid state after validation rejection without clearing the stable error', async () => {
    routeParams.slug = 'vrchat'
    get
      .mockResolvedValueOnce({
        ...VRCHAT,
        disabled: true,
        secretConfigured: true,
        secretStatus: 'valid',
        secretValidatedAt: '2026-07-17T11:00:00Z',
        ready: true,
      })
      .mockResolvedValueOnce({
        ...VRCHAT,
        disabled: true,
        secretConfigured: true,
        secretStatus: 'invalid',
        secretValidatedAt: '2026-07-17T11:00:00Z',
        ready: false,
      })
    post.mockRejectedValue({ code: 'vrchat_operator_credentials_invalid' })
    const w = mountView()
    await flushPromises()
    await w.get('[data-test="operator-validate"]').trigger('click')
    await flushPromises()

    expect(get).toHaveBeenNthCalledWith(2, '/api/prohibitorum/identity-providers/vrchat')
    expect(w.text()).toContain(en.errors.codes.vrchat_operator_credentials_invalid)
    expect(w.get('[data-test="operator-status-badge"]').text()).toBe(en.admin.upstream.operatorStatusInvalid)
    expect(w.find('input[name="operatorUsername"]').exists()).toBe(true)
    expect(w.find('input[name="operatorPassword"]').exists()).toBe(true)
    expect(w.get('[data-test="disable-toggle"]').attributes('disabled')).toBeDefined()
    expect(w.text()).toContain(en.admin.upstream.operatorEnableRequiresValid)
  })

  it('projects invalid state before a deferred refresh releases busy', async () => {
    routeParams.slug = 'vrchat'
    const refresh = deferred<typeof VRCHAT>()
    get
      .mockResolvedValueOnce({
        ...VRCHAT,
        secretConfigured: true,
        secretStatus: 'valid',
        ready: true,
      })
      .mockReturnValueOnce(refresh.promise)
    post.mockRejectedValue({ code: 'vrchat_operator_credentials_invalid' })
    const w = mountView()
    await flushPromises()
    await w.get('[data-test="operator-validate"]').trigger('click')
    await w.vm.$nextTick()

    expect(w.get('[data-test="operator-status-badge"]').text()).toBe(en.admin.upstream.operatorStatusInvalid)
    expect(w.get('[data-test="operator-start"]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-test="disable-toggle"]').attributes('disabled')).toBeDefined()
    refresh.resolve({ ...VRCHAT, secretConfigured: true, secretStatus: 'invalid' })
    await flushPromises()
    expect(w.text()).toContain(en.errors.codes.vrchat_operator_credentials_invalid)
    expect(document.activeElement).toBe(w.get('input[name="operatorUsername"]').element)
  })

  it('keeps projected invalid setup and the original error when refresh fails', async () => {
    routeParams.slug = 'vrchat'
    get
      .mockResolvedValueOnce({
        ...VRCHAT,
        secretConfigured: true,
        secretStatus: 'valid',
        ready: true,
      })
      .mockRejectedValueOnce({ code: 'network_error' })
    post.mockRejectedValue({ code: 'vrchat_operator_credentials_invalid' })
    const w = mountView()
    await flushPromises()
    await w.get('[data-test="operator-validate"]').trigger('click')
    await flushPromises()

    expect(w.text()).toContain(en.errors.codes.vrchat_operator_credentials_invalid)
    expect(w.get('[data-test="operator-status-badge"]').text()).toBe(en.admin.upstream.operatorStatusInvalid)
    expect(w.find('input[name="operatorUsername"]').exists()).toBe(true)
    expect(w.get('[data-test="disable-toggle"]').attributes('disabled')).toBeDefined()
  })

  it('snapshots credentials before delayed sudo and skips API work after unmount', async () => {
    post.mockResolvedValue({
      status: 'valid',
      provider: {
        ...VRCHAT,
        secretConfigured: true,
        secretStatus: 'valid',
        ready: true,
      },
    })
    const firstGate = deferred<void>()
    withSudo.mockImplementationOnce(async (callback) => {
      await firstGate.promise
      return callback()
    })
    const w = await mountVrchat()
    await w.get('input[name="operatorUsername"]').setValue('snapshot-user')
    await w.get('input[name="operatorPassword"]').setValue('snapshot-password')
    await w.get('[data-test="operator-start"]').trigger('click')
    await w.get('input[name="operatorUsername"]').setValue('mutated-user')
    await w.get('input[name="operatorPassword"]').setValue('mutated-password')
    firstGate.resolve()
    await flushPromises()
    expect(post).toHaveBeenLastCalledWith(
      '/api/prohibitorum/identity-providers/vrchat/operator-session/start',
      { username: 'snapshot-user', password: 'snapshot-password' },
    )

    post.mockClear()
    const unmountGate = deferred<void>()
    withSudo.mockImplementationOnce(async (callback) => {
      await unmountGate.promise
      return callback()
    })
    await w.get('[data-test="operator-replace"]').trigger('click')
    await w.get('input[name="operatorUsername"]').setValue('discarded-user')
    await w.get('input[name="operatorPassword"]').setValue('discarded-password')
    await w.get('[data-test="operator-start"]').trigger('click')
    w.unmount()
    unmountGate.resolve()
    await flushPromises()
    expect(post).not.toHaveBeenCalled()
  })

  it('still permits disabling an active VRChat provider with an invalid session', async () => {
    post.mockResolvedValue({ ...VRCHAT, disabled: true, secretStatus: 'invalid' })
    const w = await mountVrchat({ disabled: false, secretStatus: 'invalid' })
    expect(w.get('[data-test="disable-toggle"]').attributes('disabled')).toBeUndefined()
    await w.get('[data-test="disable-toggle"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith(
      '/api/prohibitorum/identity-providers/set-disabled',
      { slug: 'vrchat', disabled: true },
    )
  })
  it('localizes provider_not_ready when enabling is rejected', async () => {
    get.mockResolvedValue({ ...IDP, disabled: true, ready: false, secretStatus: 'invalid' })
    post.mockRejectedValue({ code: 'provider_not_ready', message: 'raw server message' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="disable-toggle"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.errors.codes.provider_not_ready)
    expect(w.find('[data-test="status-badge"]').text()).toBe(en.admin.upstream.disabled)
  })
})
