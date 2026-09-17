import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const { withSudo } = vi.hoisted(() => ({ withSudo: vi.fn((fn: () => Promise<unknown>) => fn()) }))
vi.mock('@/lib/sudo', () => ({ withSudo }))

const get = vi.mocked(api.get)
const post = vi.mocked(api.post)
const put = vi.mocked(api.put)
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
  useRoute: () => ({ params: { clientId: 'edge' } }),
}))

import AdminForwardAuthAppDetailView from './AdminForwardAuthAppDetailView.vue'

const integrationStubs = {
  RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' },
  AppPolicyWorkspace: {
    props: ['kind', 'appId', 'mode'],
    template: '<section data-test="app-policy-workspace" :data-kind="kind" :data-app-id="appId" :data-mode="mode"></section>',
  },
  AppManagerCard: {
    props: ['kind', 'appId'],
    template: '<section data-test="app-manager-card" :data-kind="kind" :data-app-id="appId"></section>',
  },
}
const mountView = () => mount(AdminForwardAuthAppDetailView, {
  global: {
    plugins: [createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })],
    stubs: integrationStubs,
  },
  attachTo: document.body,
})
const APP = {
  clientId: 'edge',
  displayName: 'Edge Proxy',
  forwardAuthHost: 'edge.example.test',
  accessRestricted: false,
  disabled: false,
  createdAt: '2026-01-01T00:00:00Z',
  scopes: [{ name: 'team', description: 'Allowed team claim' }],
  remoteUserSource: 'username' as const,
}

beforeEach(() => {
  get.mockReset()
  post.mockReset()
  put.mockReset()
  push.mockReset()
  withSudo.mockClear()
})

describe('AdminForwardAuthAppDetailView', () => {
  it('loads forward-auth configuration and saves changed values via PUT', async () => {
    get.mockResolvedValue(APP)
    put.mockResolvedValue({ ...APP, displayName: 'Renamed Edge Proxy' })
    const w = mountView(); await flushPromises()

    expect(get).toHaveBeenCalledWith('/api/prohibitorum/forward-auth-apps/edge', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(w.find('[data-test="fa-client-id"]').text()).toBe('edge')
    expect(w.find<HTMLInputElement>('input[name="host"]').element.value).toBe('edge.example.test')
    await w.find('input[name="displayName"]').setValue('Renamed Edge Proxy')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/forward-auth-apps/edge', {
      displayName: 'Renamed Edge Proxy',
      host: 'edge.example.test',
      scopes: APP.scopes,
    })
    expect(w.text()).toContain(en.admin.forwardAuth.saved)
  })

  it('disables the app through its dedicated endpoint and flips the status badge', async () => {
    get.mockResolvedValue(APP)
    post.mockResolvedValue({ ...APP, disabled: true })
    const w = mountView(); await flushPromises()

    expect(w.find('[data-test="status-badge"]').text()).toBe(en.admin.forwardAuth.active)
    await w.find('[data-test="disable-toggle"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/forward-auth-apps/set-disabled', { clientId: 'edge', disabled: true })
    expect(w.find('[data-test="status-badge"]').text()).toBe(en.admin.forwardAuth.disabled)
  })

  it('saves the Remote-User source without fresh sudo', async () => {
    get.mockResolvedValue(APP)
    put.mockResolvedValue(APP)
    const w = mountView(); await flushPromises()
    expect(w.get('[data-test="remote-user-source"]').text()).toContain(en.admin.forwardAuth.principalUsername)
    await w.get('[data-test="save-identity-projection"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/forward-auth-apps/edge/identity-projection', { remoteUserSource: 'username' })
    expect(withSudo).not.toHaveBeenCalled()
    expect(w.text()).toContain(en.admin.forwardAuth.identityProjectionSaved)
  })

  it('deletes the app and returns to the forward-auth application list', async () => {
    get.mockResolvedValue(APP)
    post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()

    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    const destructiveButtons = Array.from(document.body.querySelectorAll('button')).filter((button) =>
      button.getAttribute('data-variant') === 'destructive' && button.textContent?.includes(en.admin.forwardAuth.delete),
    )
    destructiveButtons[destructiveButtons.length - 1]!.click()
    await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/forward-auth-apps/delete', { clientId: 'edge' })
    expect(push).toHaveBeenCalledWith('/admin/forward-auth-apps')
  })

  it('keeps forward-auth configuration while embedding the admin policy workspace and separate manager card', async () => {
    get.mockResolvedValue(APP)
    const w = mountView(); await flushPromises()

    expect(w.find('[data-test="fa-client-id"]').text()).toBe('edge')
    expect(w.find<HTMLInputElement>('input[name="host"]').element.value).toBe('edge.example.test')

    const workspace = w.get('[data-test="app-policy-workspace"]')
    expect(workspace.attributes('data-kind')).toBe('forward_auth')
    expect(workspace.attributes('data-app-id')).toBe('edge')
    expect(workspace.attributes('data-mode')).toBe('admin')

    const managerCard = w.get('[data-test="app-manager-card"]')
    expect(managerCard.attributes('data-kind')).toBe('forward_auth')
    expect(managerCard.attributes('data-app-id')).toBe('edge')

    const configCard = w.findAll('[data-slot="card"]').find((card) => card.find('[data-test="save"]').exists())
    expect(configCard).toBeTruthy()
    expect(configCard!.find('[data-test="app-manager-card"]').exists()).toBe(false)
  })
})
