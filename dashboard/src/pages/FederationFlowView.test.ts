import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import en from '@/locales/en'
import FederationFlowView from './FederationFlowView.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }))
import { api } from '@/lib/api'

const { hardRedirect } = vi.hoisted(() => ({ hardRedirect: vi.fn() }))
vi.mock('@/lib/navigate', () => ({ hardRedirect }))

const get = vi.mocked(api.get)
const post = vi.mocked(api.post)
const FLOW = 'flow_abc'
const basePath = `/api/prohibitorum/auth/federation/flows/${FLOW}`
const expiresAt = '2099-04-05T12:30:00Z'

const identifyFlow = {
  provider: { slug: 'vrchat', displayName: 'VRChat', protocol: 'vrchat' },
  intent: 'login',
  step: 'identify' as const,
  requiresLocalUsername: false,
  expiresAt,
}

const proofFlow = {
  ...identifyFlow,
  step: 'proof' as const,
  profileUrl: 'https://vrchat.com/home/user/usr_12345678-1234-1234-1234-123456789abc',
  proofUrl: 'https://id.example.test/verify/vrchat/proof_very_long_one_time_value',
}

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

async function makeRouter(): Promise<Router> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/federation/flow/:flow', component: FederationFlowView }],
  })
  await router.push(`/federation/flow/${FLOW}`)
  await router.isReady()
  return router
}

const mounted: VueWrapper[] = []
async function mountView(settle = true): Promise<VueWrapper> {
  const wrapper = mount(FederationFlowView, {
    attachTo: document.body,
    global: { plugins: [await makeRouter(), makeI18n()] },
  })
  mounted.push(wrapper)
  if (settle) await flushPromises()
  return wrapper
}

beforeEach(() => {
  setActivePinia(createPinia())
  get.mockReset()
  post.mockReset()
  hardRedirect.mockReset()
  Object.assign(navigator, { clipboard: { writeText: vi.fn(async () => {}) } })
})

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount()
})

describe('FederationFlowView', () => {
  it('shows a skeleton while the initial GET is pending', async () => {
    get.mockReturnValue(Promise.withResolvers<never>().promise)
    const wrapper = await mountView(false)

    expect(get).toHaveBeenCalledWith(basePath)
    expect(wrapper.get('[data-test="flow-loading"]').attributes('aria-busy')).toBe('true')
    expect(wrapper.findAll('[data-slot="skeleton"]').length).toBeGreaterThan(0)
  })

  it('labels the VRChat identity field with an example and submits it from the form', async () => {
    get.mockResolvedValue(identifyFlow)
    post.mockResolvedValue(proofFlow)
    const wrapper = await mountView()

    expect(wrapper.get('label[for="federation-identity"]').text()).toBe('VRChat user ID or profile URL')
    expect(wrapper.text()).toContain('usr_12345678-1234-1234-1234-123456789abc')

    await wrapper.get('input[name="identity"]').setValue('usr_12345678-1234-1234-1234-123456789abc')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(`${basePath}/prepare`, {
      identity: 'usr_12345678-1234-1234-1234-123456789abc',
    })
    expect(wrapper.get('[data-test="proof-heading"]').text()).toContain('Add the one-time link')
    expect(document.activeElement).toBe(wrapper.get('[data-test="proof-heading"]').element)
  })

  it('renders the proof profile, copyable URL, ordered instructions, expiry, and no credential fields', async () => {
    get.mockResolvedValue(proofFlow)
    const wrapper = await mountView()

    const profile = wrapper.get('[data-test="profile-link"]')
    expect(profile.attributes('href')).toBe(proofFlow.profileUrl)
    expect(profile.attributes('target')).toBe('_blank')
    expect(profile.text()).toContain(proofFlow.profileUrl)
    expect(profile.get('.font-mono').classes()).toContain('break-all')
    expect(wrapper.get('[data-test="locale-trigger"]').classes()).toContain('h-11')
    expect(wrapper.get('code').text()).toBe(proofFlow.proofUrl)
    expect(wrapper.findAll('ol li')).toHaveLength(3)
    expect(wrapper.text()).toContain('Expires')
    expect(wrapper.find('input[name="localUsername"]').exists()).toBe(false)
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(wrapper.text()).not.toMatch(/VRChat (password|credentials|2FA|cookie)/i)
    expect(get).toHaveBeenCalledTimes(1)
  })

  it('shows the local username only when the flow requires it', async () => {
    get.mockResolvedValue({ ...proofFlow, requiresLocalUsername: true })
    const wrapper = await mountView()

    expect(wrapper.get('label[for="local-username"]').text()).toBe('Local username')
    expect(wrapper.get('input[name="localUsername"]').attributes('autocomplete')).toBe('username')
  })

  it('keeps proof controls available and focuses Verify profile when the bio link is missing', async () => {
    get.mockResolvedValue(proofFlow)
    post.mockRejectedValue({ code: 'vrchat_proof_missing' })
    const wrapper = await mountView()

    await wrapper.get('[data-test="verify-profile"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[role="alert"]').text()).toContain('Add the issued verification link')
    expect(wrapper.get('code').text()).toBe(proofFlow.proofUrl)
    expect(wrapper.get('[data-test="verify-profile"]').attributes('disabled')).toBeUndefined()
    expect(document.activeElement).toBe(wrapper.get('[data-test="verify-profile"]').element)
  })

  it('reloads a changed flow after local_username_required, preserves proof, and focuses username', async () => {
    get.mockResolvedValueOnce(proofFlow).mockResolvedValueOnce({
      ...proofFlow,
      requiresLocalUsername: true,
    })
    post.mockRejectedValue({ code: 'local_username_required' })
    const wrapper = await mountView()

    await wrapper.get('[data-test="verify-profile"]').trigger('click')
    await flushPromises()

    expect(get).toHaveBeenNthCalledWith(2, basePath)
    expect(post).toHaveBeenCalledWith(`${basePath}/verify`)
    expect(wrapper.get('code').text()).toBe(proofFlow.proofUrl)
    expect(wrapper.get('input[name="localUsername"]').exists()).toBe(true)
    expect(document.activeElement).toBe(wrapper.get('input[name="localUsername"]').element)
  })

  it('preserves proof and focuses local username after username_collision', async () => {
    get.mockResolvedValue({ ...proofFlow, requiresLocalUsername: true })
    post.mockRejectedValue({ code: 'username_collision' })
    const wrapper = await mountView()

    await wrapper.get('input[name="localUsername"]').setValue('alex')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('code').text()).toBe(proofFlow.proofUrl)
    expect(wrapper.get('[role="alert"]').text()).toContain('already taken')
    expect(document.activeElement).toBe(wrapper.get('input[name="localUsername"]').element)
  })

  it('shows Retry-After timing for an upstream rate limit without hiding proof', async () => {
    get.mockResolvedValue(proofFlow)
    post.mockRejectedValue({ code: 'upstream_rate_limited', retryAfterSeconds: 37 })
    const wrapper = await mountView()

    await wrapper.get('[data-test="verify-profile"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Try again in 37 seconds.')
    expect(wrapper.get('code').text()).toBe(proofFlow.proofUrl)
  })

  it('uses the terminal ErrorPanel when the flow GET fails', async () => {
    get.mockRejectedValue({ code: 'federation_state_invalid' })
    const wrapper = await mountView()

    expect(wrapper.get('[role="alert"]').text()).toContain('expired')
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.find('[data-test="error-dismiss"]').exists()).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toContain('expired')
  })

  it('announces clipboard success and failure while leaving the URL visible', async () => {
    get.mockResolvedValue(proofFlow)
    const writeText = vi.fn(async () => {})
    Object.assign(navigator, { clipboard: { writeText } })
    const wrapper = await mountView()

    await wrapper.get('[data-test="copy-code"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(proofFlow.proofUrl)
    expect(wrapper.text()).toContain('Copied')

    writeText.mockRejectedValueOnce(new Error('blocked'))
    await wrapper.get('[data-test="copy-code"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Copy failed. Select and copy the value manually.')
    expect(wrapper.get('code').text()).toBe(proofFlow.proofUrl)
  })

  it('announces verification success without automatic navigation and redirects only on Continue', async () => {
    get.mockResolvedValue(proofFlow)
    post.mockResolvedValue({ redirect: '/welcome' })
    const wrapper = await mountView()

    await wrapper.get('[data-test="verify-profile"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="verification-status"]').text()).toContain('Profile verified — remove the bio link now')
    expect(document.activeElement).toBe(wrapper.get('[data-test="success-heading"]').element)
    expect(hardRedirect).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="verify-profile"]').exists()).toBe(false)

    await wrapper.get('[data-test="continue"]').trigger('click')
    expect(hardRedirect).toHaveBeenCalledWith('/welcome')
  })

  it('submits the optional username and exposes accessible action names', async () => {
    get.mockResolvedValue({ ...proofFlow, requiresLocalUsername: true })
    post.mockResolvedValue({ redirect: '/' })
    const wrapper = await mountView()

    await wrapper.get('input[name="localUsername"]').setValue('alex')
    expect(wrapper.get('[data-test="verify-profile"]').text()).toBe('Verify profile')
    expect(wrapper.get('[data-test="profile-link"]').attributes('aria-label')).toBe(`Open VRChat profile: ${proofFlow.profileUrl}`)
    expect(wrapper.get('[data-test="copy-code"]').attributes('aria-label')).toBe('Copy one-time proof URL')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(`${basePath}/verify`, { localUsername: 'alex' })
  })
})
