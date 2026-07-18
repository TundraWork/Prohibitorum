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

  it('explains the local account handoff before asking for a VRChat profile', async () => {
    get.mockResolvedValue(identifyFlow)
    const wrapper = await mountView()

    const notice = wrapper.get('[data-test="account-handoff-notice"]')
    expect(notice.text()).toContain(
      'Your VRChat account is only used to verify your identity and help you recover access. If you’re new here, you’ll create a local account and sign-in method after verification.',
    )
    expect(notice.text()).toContain(
      'Can you still sign in to your local account? Link VRChat from Connected Accounts instead.',
    )
    expect(notice.classes()).toEqual(
      expect.arrayContaining([
        'min-w-0',
        'border-info-border',
        'bg-info',
        'text-info-foreground',
      ]),
    )
    expect(
      notice.element.compareDocumentPosition(wrapper.get('input[name="identity"]').element),
    ).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
    expect(wrapper.findAll('h1')).toHaveLength(1)
  })

  it('shows how to find a profile URL and submits the unchanged identity value', async () => {
    get.mockResolvedValue(identifyFlow)
    post.mockResolvedValue(proofFlow)
    const wrapper = await mountView()

    const guide = wrapper.get('[data-test="identify-guide"]')
    expect(guide.get('h2').text()).toBe(en.federationFlow.identifyGuideTitle)
    expect(guide.findAll('ol li')).toHaveLength(3)
    expect(guide.text()).toContain(en.federationFlow.identifyStepOpen)
    expect(guide.text()).toContain(en.federationFlow.identifyStepProfile)
    expect(guide.text()).toContain(en.federationFlow.identifyStepCopy)
    expect(wrapper.get('[data-test="open-vrchat"]').attributes('href')).toBe('https://vrchat.com/home')
    expect(wrapper.get('[data-test="open-vrchat"]').attributes('target')).toBe('_blank')
    expect(wrapper.text()).toContain(en.federationFlow.noCredentials)
    expect(wrapper.get('label[for="federation-identity"]').text()).toBe(en.federationFlow.identityLabel)
    expect(wrapper.get('input[name="identity"]').attributes('placeholder')).toBe(en.federationFlow.identityPlaceholder)

    const value = 'https://vrchat.com/home/user/usr_12345678-1234-1234-1234-123456789abc'
    await wrapper.get('input[name="identity"]').setValue(value)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(post).toHaveBeenCalledWith(`${basePath}/prepare`, { identity: value })
  })

  it('renders the proof profile, copyable URL, ordered instructions, expiry, and no credential fields', async () => {
    get.mockResolvedValue(proofFlow)
    const wrapper = await mountView()

    const profile = wrapper.get('[data-test="profile-link"]')
    expect(profile.attributes('href')).toBe(proofFlow.profileUrl)
    expect(profile.attributes('target')).toBe('_blank')
    expect(profile.text()).toBe(en.federationFlow.openProfile)
    expect(wrapper.get('[data-test="profile-context"] code').text()).toBe('usr_12345678-1234-1234-1234-123456789abc')
    expect(wrapper.get('[data-test="locale-trigger"]').classes()).toContain('h-11')
    expect(wrapper.get('[data-test="proof-url"] code').text()).toBe(proofFlow.proofUrl)
    expect(wrapper.findAll('ol li')).toHaveLength(3)
    expect(wrapper.text()).toContain('Expires')
    expect(wrapper.find('input[name="localUsername"]').exists()).toBe(false)
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(wrapper.text()).not.toMatch(/VRChat (password|credentials|2FA|cookie)/i)
    expect(get).toHaveBeenCalledTimes(1)
  })

  it('presents profile context, proof steps, and expiry as one ordered task', async () => {
    get.mockResolvedValue(proofFlow)
    const wrapper = await mountView()

    const context = wrapper.get('[data-test="profile-context"]')
    expect(context.text()).toContain('usr_12345678-1234-1234-1234-123456789abc')
    const profileLink = context.get('[data-test="profile-link"]')
    expect(profileLink.text()).toContain(en.federationFlow.openProfile)
    expect(profileLink.attributes('href')).toBe(proofFlow.profileUrl)

    expect(wrapper.get('[data-test="copy-code"]').attributes('aria-label')).toBe(en.federationFlow.copyProofUrl)
    const steps = wrapper.get('[data-test="proof-steps"]')
    expect(steps.element.tagName).toBe('OL')
    expect(steps.findAll('li')).toHaveLength(3)
    expect(wrapper.get('[data-test="proof-expiry"]').text()).toContain('Expires')
    expect(wrapper.get('[data-test="verify-profile"]').text()).toBe(en.federationFlow.verify)
  })

  it('never asks for or submits a local username in the public proof flow', async () => {
    get.mockResolvedValue({ ...proofFlow, requiresLocalUsername: true })
    post.mockResolvedValue({ redirect: '/enroll/enroll_abc' })
    const wrapper = await mountView()

    expect(wrapper.find('input[name="localUsername"]').exists()).toBe(false)

    await wrapper.get('[data-test="verify-profile"]').trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(`${basePath}/verify`)
  })

  it('keeps proof controls available and focuses Verify profile when the bio link is missing', async () => {
    get.mockResolvedValue(proofFlow)
    post.mockRejectedValue({ code: 'vrchat_proof_missing' })
    const wrapper = await mountView()

    await wrapper.get('[data-test="verify-profile"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[role="alert"]').text()).toContain('Add the issued verification link')
    expect(wrapper.get('[data-test="proof-url"] code').text()).toBe(proofFlow.proofUrl)
    expect(wrapper.get('[data-test="verify-profile"]').attributes('disabled')).toBeUndefined()
    expect(document.activeElement).toBe(wrapper.get('[data-test="verify-profile"]').element)
  })


  it('shows Retry-After timing for an upstream rate limit without hiding proof', async () => {
    get.mockResolvedValue(proofFlow)
    post.mockRejectedValue({ code: 'upstream_rate_limited', retryAfterSeconds: 37 })
    const wrapper = await mountView()

    await wrapper.get('[data-test="verify-profile"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Try again in 37 seconds.')
    expect(wrapper.get('[data-test="proof-url"] code').text()).toBe(proofFlow.proofUrl)
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
    expect(wrapper.get('[data-test="proof-url"] code').text()).toBe(proofFlow.proofUrl)
  })

  it('keeps enrollment navigation behind the existing success and Continue interaction', async () => {
    get.mockResolvedValue(proofFlow)
    post.mockResolvedValue({ redirect: '/enroll/enroll_abc' })
    const wrapper = await mountView()

    await wrapper.get('[data-test="verify-profile"]').trigger('click')
    await flushPromises()

    const success = wrapper.get('[data-test="verification-status"]')
    expect(success.text()).toContain('Profile verified')
    expect(success.text()).not.toMatch(/signed in|local session/i)
    expect(document.activeElement).toBe(wrapper.get('[data-test="success-heading"]').element)
    expect(hardRedirect).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="verify-profile"]').exists()).toBe(false)

    await wrapper.get('[data-test="continue"]').trigger('click')
    expect(hardRedirect).toHaveBeenCalledWith('/enroll/enroll_abc')
  })

  it('keeps authenticated linking on the Connected Accounts return path', async () => {
    get.mockResolvedValue({ ...proofFlow, intent: 'link' })
    post.mockResolvedValue({ redirect: '/connected' })
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="verify-profile"]').text()).toBe('Verify profile')
    expect(wrapper.get('[data-test="profile-link"]').attributes('aria-label')).toBe('Open VRChat profile: usr_12345678-1234-1234-1234-123456789abc')
    expect(wrapper.get('[data-test="copy-code"]').attributes('aria-label')).toBe('Copy one-time proof URL')
    expect(wrapper.text()).not.toMatch(/signed in|local session/i)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('[data-test="continue"]').trigger('click')

    expect(post).toHaveBeenCalledWith(`${basePath}/verify`)
    expect(hardRedirect).toHaveBeenCalledWith('/connected')
  })
})
