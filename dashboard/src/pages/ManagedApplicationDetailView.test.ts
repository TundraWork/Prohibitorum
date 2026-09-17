import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { testQueryClient } from '@/testSetup'
import { keys, type SessionView } from '@/queries/resources'
import ManagedApplicationDetailView from './ManagedApplicationDetailView.vue'

const stub = { template: '<div />' }
const protocolStubs = {
  AdminOidcClientDetailView: { props: ['mode', 'currentAccountId'], template: '<section data-test="oidc-detail" :data-mode="mode" :data-account-id="currentAccountId" />' },
  AdminForwardAuthAppDetailView: { props: ['mode', 'currentAccountId'], template: '<section data-test="forward-auth-detail" :data-mode="mode" :data-account-id="currentAccountId" />' },
  AdminSamlProviderDetailView: { props: ['mode', 'currentAccountId'], template: '<section data-test="saml-detail" :data-mode="mode" :data-account-id="currentAccountId" />' },
}

async function mountView(path: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/manage/applications', component: stub },
      { path: '/manage/applications/:kind/:id', component: ManagedApplicationDetailView },
    ],
  })
  await router.push(path); await router.isReady()
  return mount(ManagedApplicationDetailView, {
    global: { plugins: [router], stubs: protocolStubs },
  })
}

describe('ManagedApplicationDetailView', () => {
  it.each([
    ['oidc', 'client%2Fid', 'oidc-detail'],
    ['forward_auth', 'edge', 'forward-auth-detail'],
    ['saml', '44', 'saml-detail'],
  ])('dispatches %s applications to the full protocol editor in manager mode', async (kind, id, testId) => {
    testQueryClient.setQueryData<SessionView>(keys.me, { id: 17, username: 'manager', displayName: 'Manager', role: 'app_manager' })
    const wrapper = await mountView(`/manage/applications/${kind}/${id}`)

    const detail = wrapper.get(`[data-test="${testId}"]`)
    expect(detail.attributes('data-mode')).toBe('manager')
    expect(detail.attributes('data-account-id')).toBe('17')
    expect(wrapper.findAll('[data-mode="manager"]')).toHaveLength(1)
  })

  it('does not render a protocol editor for an unsupported application kind', async () => {
    const wrapper = await mountView('/manage/applications/unknown/value')

    expect(wrapper.get('[data-test="managed-application-detail"]').element.children).toHaveLength(0)
  })
})
