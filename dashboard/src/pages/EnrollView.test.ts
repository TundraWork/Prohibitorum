import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createRouter, createMemoryHistory, type Router } from 'vue-router'
import { defineComponent } from 'vue'
import en from '@/locales/en'
import EnrollView from './EnrollView.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'

vi.mock('@/lib/webauthn', () => ({
  passkeyGet: vi.fn(),
  passkeyRegister: vi.fn(async () => ({ id: 'cred', response: {} })),
  isUserCancel: () => false,
}))

const { hardRedirect } = vi.hoisted(() => ({ hardRedirect: vi.fn() }))
vi.mock('@/lib/navigate', () => ({ hardRedirect }))

const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

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
  get.mockReset()
  post.mockReset()
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

  it('routes to /error when the preview fails', async () => {
    get.mockRejectedValue({ code: 'enrollment_expired', message: '已过期' })
    const router = await makeRouter()
    await mountView(router)

    expect(router.currentRoute.value.name).toBe('error')
    expect(router.currentRoute.value.query.error).toBe('enrollment_expired')
  })
})
