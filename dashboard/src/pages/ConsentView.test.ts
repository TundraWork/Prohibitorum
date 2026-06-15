import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createRouter, createMemoryHistory, type Router } from 'vue-router'
import { defineComponent } from 'vue'
import en from '@/locales/en'
import ConsentView from './ConsentView.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'

const { hardRedirect } = vi.hoisted(() => ({ hardRedirect: vi.fn() }))
vi.mock('@/lib/navigate', () => ({ hardRedirect }))

const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

const stub = defineComponent({ template: '<div/>' })
const AUTHORIZE_URL = 'https://idp.example/authorize?client_id=app&state=xyz'

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

async function makeRouter(query = `?ticket=tkt_1&return_to=${encodeURIComponent(AUTHORIZE_URL)}`): Promise<Router> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/consent', name: 'consent', component: ConsentView },
      { path: '/login', name: 'login', component: stub },
      { path: '/error', name: 'error', component: stub },
    ],
  })
  router.push(`/consent${query}`)
  await router.isReady()
  return router
}

async function mountView(router: Router) {
  const wrapper = mount(ConsentView, { global: { plugins: [router, makeI18n()] } })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  get.mockReset()
  post.mockReset()
  hardRedirect.mockReset()
})

describe('ConsentView', () => {
  it('renders client, account and scopes from the context', async () => {
    get.mockResolvedValue({
      client: { clientId: 'app', displayName: 'Demo App' },
      account: { displayName: 'Alex Smith' },
      scopes: ['openid', 'profile', 'custom:thing'],
    })
    const wrapper = await mountView(await makeRouter())

    expect(get).toHaveBeenCalledWith('/api/prohibitorum/consent?ticket=tkt_1')
    expect(wrapper.text()).toContain('Demo App')
    expect(wrapper.text()).toContain('Alex Smith')
    // known scope → described; unknown scope → raw in <code> with custom-scope sub-label.
    expect(wrapper.text()).toContain(en.consent.scopes.openid)
    expect(wrapper.find('code').text()).toBe('custom:thing')
    expect(wrapper.text()).toContain(en.consent.customScope)
  })

  it('renders logo when logoUri is present', async () => {
    get.mockResolvedValue({
      client: { clientId: 'app', displayName: 'Demo App', logoUri: 'https://app.example/logo.png' },
      account: { displayName: 'Alex' },
      scopes: ['openid'],
    })
    const wrapper = await mountView(await makeRouter())
    const img = wrapper.find('img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toBe('https://app.example/logo.png')
    expect(img.attributes('alt')).toBe('Demo App')
  })

  it('does not render logo when logoUri is absent', async () => {
    get.mockResolvedValue({
      client: { clientId: 'app', displayName: 'Demo App' },
      account: { displayName: 'Alex' },
      scopes: ['openid'],
    })
    const wrapper = await mountView(await makeRouter())
    expect(wrapper.find('img').exists()).toBe(false)
  })

  it('renders policy and ToS links when present', async () => {
    get.mockResolvedValue({
      client: {
        clientId: 'app', displayName: 'Demo App',
        policyUri: 'https://app.example/privacy',
        tosUri: 'https://app.example/terms',
      },
      account: { displayName: 'Alex' },
      scopes: ['openid'],
    })
    const wrapper = await mountView(await makeRouter())
    const links = wrapper.findAll('a[target="_blank"]')
    const hrefs = links.map((l) => l.attributes('href'))
    expect(hrefs).toContain('https://app.example/privacy')
    expect(hrefs).toContain('https://app.example/terms')
    expect(wrapper.text()).toContain(en.consent.privacyPolicy)
    expect(wrapper.text()).toContain(en.consent.termsOfService)
  })

  it('does not render policy/ToS section when both are absent', async () => {
    get.mockResolvedValue({
      client: { clientId: 'app', displayName: 'Demo App' },
      account: { displayName: 'Alex' },
      scopes: ['openid'],
    })
    const wrapper = await mountView(await makeRouter())
    expect(wrapper.find('a[target="_blank"]').exists()).toBe(false)
  })

  it('approve button shows the scope count', async () => {
    get.mockResolvedValue({
      client: { clientId: 'app', displayName: 'Demo App' },
      account: { displayName: 'Alex' },
      scopes: ['openid', 'profile', 'email'],
    })
    const wrapper = await mountView(await makeRouter())
    const approveBtn = wrapper.findAll('button').find((b) => b.text().includes('Allow access'))
    expect(approveBtn).toBeTruthy()
    expect(approveBtn!.text()).toContain('3')
  })

  it('approve posts the decision with return_to and follows the server redirect', async () => {
    get.mockResolvedValue({
      client: { clientId: 'app', displayName: 'Demo App' },
      account: { displayName: 'Alex' },
      scopes: ['openid'],
    })
    post.mockResolvedValue({ redirect: AUTHORIZE_URL })
    const wrapper = await mountView(await makeRouter())

    const approve = wrapper.findAll('button').find((b) => b.text().includes('Allow access'))!
    await approve.trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(
      `/api/prohibitorum/consent?return_to=${encodeURIComponent(AUTHORIZE_URL)}`,
      { ticket: 'tkt_1', decision: 'approve' },
    )
    expect(hardRedirect).toHaveBeenCalledWith(AUTHORIZE_URL)
  })

  it('deny follows the server redirect (the RP access_denied URL)', async () => {
    const rpDenied = 'https://rp.example/cb?error=access_denied&state=xyz'
    get.mockResolvedValue({
      client: { clientId: 'app', displayName: 'Demo App' },
      account: { displayName: 'Alex' },
      scopes: ['openid'],
    })
    post.mockResolvedValue({ redirect: rpDenied })
    const wrapper = await mountView(await makeRouter())

    const deny = wrapper.findAll('button').find((b) => b.text() === en.consent.deny)!
    await deny.trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(expect.any(String), { ticket: 'tkt_1', decision: 'deny' })
    expect(hardRedirect).toHaveBeenCalledWith(rpDenied)
  })

  it('routes to /login when there is no session', async () => {
    get.mockRejectedValue({ code: 'no_session', message: '请先登录' })
    const router = await makeRouter()
    await mountView(router)

    expect(router.currentRoute.value.name).toBe('login')
    expect(router.currentRoute.value.query.return_to).toContain('/consent')
  })

  it('routes to /error on an invalid ticket', async () => {
    get.mockRejectedValue({ code: 'invalid_consent_ticket', message: '授权请求已失效' })
    const router = await makeRouter()
    await mountView(router)

    expect(router.currentRoute.value.name).toBe('error')
    expect(router.currentRoute.value.query.error).toBe('invalid_consent_ticket')
  })
})
