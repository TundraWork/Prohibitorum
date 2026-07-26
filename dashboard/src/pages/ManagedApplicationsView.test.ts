import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createRouter, createMemoryHistory } from 'vue-router'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))
import { api } from '@/lib/api'
import ManagedApplicationsView from './ManagedApplicationsView.vue'

const get = vi.mocked(api.get)
const stub = { template: '<div />' }

function mountView() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: stub },
      { path: '/manage/applications/:kind/:id', component: stub },
    ],
  })
  return mount(ManagedApplicationsView, {
    global: {
      plugins: [router, createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })],
    },
  })
}

describe('ManagedApplicationsView', () => {
  beforeEach(() => { get.mockReset() })

  it('lists only the applications returned by the delegated-management endpoint', async () => {
    get.mockResolvedValue({
      items: [
        { kind: 'oidc', appId: 'grafana', displayName: 'Grafana', accessRestricted: true },
        { kind: 'forward_auth', appId: 'wiki', displayName: 'Wiki', accessRestricted: false },
      ],
      nextCursor: '',
    })
    const wrapper = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/managed-applications')
    expect(wrapper.text()).toContain('Grafana')
    expect(wrapper.text()).toContain('Restricted access')
    expect(wrapper.text()).toContain('Wiki')
    expect(wrapper.text()).toContain('Open access')
    const links = wrapper.findAll('a').map((link) => link.attributes('href'))
    expect(links).toContain('/manage/applications/oidc/grafana')
    expect(links).toContain('/manage/applications/forward_auth/wiki')
  })

  it('shows an assignment-specific empty state', async () => {
    get.mockResolvedValue({ items: [], nextCursor: '' })
    const wrapper = mountView(); await flushPromises()
    expect(wrapper.text()).toContain('No managed applications')
    expect(wrapper.text()).toContain('You do not have any application assignments yet.')
  })
})
