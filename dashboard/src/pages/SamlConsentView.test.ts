import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createRouter, createMemoryHistory, type Router } from 'vue-router'
import { defineComponent } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import en from '@/locales/en'
import SamlConsentView from './SamlConsentView.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'

const { hardRedirect } = vi.hoisted(() => ({ hardRedirect: vi.fn() }))
vi.mock('@/lib/navigate', () => ({ hardRedirect }))

const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

const stub = defineComponent({ template: '<div/>' })

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

async function makeRouter(query = '?ticket=tkt_saml_1'): Promise<Router> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/saml-consent', name: 'saml-consent', component: SamlConsentView },
      { path: '/login', name: 'login', component: stub },
      { path: '/error', name: 'error', component: stub },
    ],
  })
  router.push(`/saml-consent${query}`)
  await router.isReady()
  return router
}

async function mountView(router: Router) {
  const wrapper = mount(SamlConsentView, { global: { plugins: [router, makeI18n()] } })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  setActivePinia(createPinia())
  get.mockReset()
  post.mockReset()
  hardRedirect.mockReset()
})

describe('SamlConsentView', () => {
  it('renders sp name, account and attribute list from context', async () => {
    get.mockResolvedValue({
      sp: { id: '42', displayName: 'Salesforce' },
      account: { displayName: 'Jesse' },
      attributes: ['Email', 'Groups'],
    })
    const wrapper = await mountView(await makeRouter())

    expect(get).toHaveBeenCalledWith('/api/prohibitorum/saml-consent?ticket=tkt_saml_1')
    expect(wrapper.text()).toContain('Salesforce')
    expect(wrapper.text()).toContain('Jesse')
    const items = wrapper.findAll('li')
    expect(items.map((li) => li.text())).toContain('Email')
    expect(items.map((li) => li.text())).toContain('Groups')
    expect(wrapper.find('ul').exists()).toBe(true)
  })

  it('shows genericAttributes fallback and no ul when attributes is empty', async () => {
    get.mockResolvedValue({
      sp: { id: '42', displayName: 'Salesforce' },
      account: { displayName: 'Jesse' },
      attributes: [],
    })
    const wrapper = await mountView(await makeRouter())

    expect(wrapper.text()).toContain(en.samlConsent.genericAttributes)
    expect(wrapper.find('ul').exists()).toBe(false)
  })

  it('Continue button posts approve decision and hardRedirects', async () => {
    const REDIRECT = 'https://salesforce.example/saml/callback?SAMLResponse=abc'
    get.mockResolvedValue({
      sp: { id: '42', displayName: 'Salesforce' },
      account: { displayName: 'Jesse' },
      attributes: ['Email', 'Groups'],
    })
    post.mockResolvedValue({ redirect: REDIRECT })
    const wrapper = await mountView(await makeRouter())

    const continueBtn = wrapper.findAll('button').find((b) => b.text() === en.samlConsent.continue)!
    expect(continueBtn).toBeTruthy()
    await continueBtn.trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(
      '/api/prohibitorum/saml-consent',
      { ticket: 'tkt_saml_1', decision: 'approve' },
    )
    expect(hardRedirect).toHaveBeenCalledWith(REDIRECT)
  })

  it('Not now button posts decline decision and hardRedirects', async () => {
    const REDIRECT = 'https://salesforce.example/saml/callback?error=access_denied'
    get.mockResolvedValue({
      sp: { id: '42', displayName: 'Salesforce' },
      account: { displayName: 'Jesse' },
      attributes: ['Email'],
    })
    post.mockResolvedValue({ redirect: REDIRECT })
    const wrapper = await mountView(await makeRouter())

    const declineBtn = wrapper.findAll('button').find((b) => b.text() === en.samlConsent.decline)!
    expect(declineBtn).toBeTruthy()
    await declineBtn.trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(
      '/api/prohibitorum/saml-consent',
      { ticket: 'tkt_saml_1', decision: 'decline' },
    )
    expect(hardRedirect).toHaveBeenCalledWith(REDIRECT)
  })

  it('routes to /login when there is no session', async () => {
    get.mockRejectedValue({ code: 'no_session', message: '请先登录' })
    const router = await makeRouter()
    await mountView(router)

    expect(router.currentRoute.value.name).toBe('login')
    expect(router.currentRoute.value.query.return_to).toContain('/saml-consent')
  })

  it('routes to /error on an invalid ticket', async () => {
    get.mockRejectedValue({ code: 'invalid_consent_ticket', message: '授权请求已失效' })
    const router = await makeRouter()
    await mountView(router)

    expect(router.currentRoute.value.name).toBe('error')
    expect(router.currentRoute.value.query.error).toBe('invalid_consent_ticket')
  })
})
