import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createRouter, createMemoryHistory } from 'vue-router'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))
import { api } from '@/lib/api'
import ManagedApplicationDetailView from './ManagedApplicationDetailView.vue'

const get = vi.mocked(api.get)
const stub = { template: '<div />' }

async function mountView(path = '/manage/applications/oidc/client%2Fid') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/manage/applications', component: stub },
      { path: '/manage/applications/:kind/:id', component: ManagedApplicationDetailView },
    ],
  })
  await router.push(path); await router.isReady()
  return mount(ManagedApplicationDetailView, {
    global: {
      plugins: [router, createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })],
    },
  })
}

describe('ManagedApplicationDetailView', () => {
  beforeEach(() => { get.mockReset() })

  it('renders the reusable policy workspace without protocol configuration', async () => {
    const workspace = {
      app: { kind: 'oidc', appId: 'client/id', displayName: 'Grafana', accessRestricted: true },
      accessRestricted: true,
      providers: [{ slug: 'corporate' }],
      manualGroup: { id: 1, kind: 'manual', slug: 'grafana-access', displayName: 'Grafana access', exposedToDownstream: false },
      ruleGroups: [
        { id: 2, kind: 'rule', slug: 'grafana-corporate', displayName: 'Corporate users', exposedToDownstream: false, rule: { version: 1, condition: { fact: 'connection.provider', provider: 'corporate' } } },
      ],
    }
    get.mockImplementation(async (path: string) =>
      path.endsWith('/access') ? workspace : { items: [], nextCursor: '' },
    )
    const wrapper = await mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/managed-applications/oidc/client%2Fid/access')
    expect(wrapper.find('[data-test="managed-application-detail"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="app-policy-workspace"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Grafana')
    expect(wrapper.text()).toContain('Restricted access')
    expect(wrapper.text()).toContain('Manual group: Grafana access')
    expect(wrapper.text()).toContain('1 rule group')
    expect(wrapper.text()).not.toContain('Redirect URI')
    expect(get.mock.calls.filter(([path]) => path.endsWith('/access'))).toHaveLength(1)
  })

  it('uses the generic unavailable state for an unknown or unassigned application', async () => {
    get.mockRejectedValue({ code: 'client_not_found' })
    const wrapper = await mountView('/manage/applications/saml/44'); await flushPromises()
    expect(wrapper.text()).toContain('That application is unavailable or is not assigned to you.')
    expect(wrapper.find('[data-test="managed-application-detail"]').exists()).toBe(false)
  })
})
