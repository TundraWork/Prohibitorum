import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import en from '@/locales/en'
import type { AppAccessWorkspace, AppGroup } from '@/lib/appAccess'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
import AppPolicyWorkspace from './AppPolicyWorkspace.vue'

const get = vi.mocked(api.get)
const post = vi.mocked(api.post)
const put = vi.mocked(api.put)
const base = '/api/prohibitorum/managed-applications/oidc/client%2Falpha'
const groups: AppGroup[] = [
  { id: 10, kind: 'manual', slug: 'exceptions', displayName: 'Exceptions', exposedToDownstream: false },
  { id: 21, kind: 'rule', slug: 'staff', displayName: 'Corporate staff', exposedToDownstream: true },
]
const workspace: AppAccessWorkspace = {
  app: { kind: 'oidc', appId: 'client/alpha', displayName: 'Atlas', accessRestricted: true },
  accessRestricted: true, providers: [], groups: [groups[1]!],
}
const mounted: VueWrapper[] = []

function mountWorkspace(mode: 'manager' | 'admin' = 'manager') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }, { path: '/admin/groups/:id', component: { template: '<div />' } }] })
  const wrapper = mount(AppPolicyWorkspace, {
    props: { kind: 'oidc', appId: 'client/alpha', displayName: 'Atlas', mode },
    global: { plugins: [router, createI18n({ legacy: false, locale: 'en', messages: { en } })] },
  })
  mounted.push(wrapper)
  return wrapper
}

beforeEach(() => {
  get.mockReset(); post.mockReset(); put.mockReset()
  get.mockImplementation(async (path: string) => {
    if (path === `${base}/access`) return workspace
    if (path === '/api/prohibitorum/groups?limit=100') return { items: groups, nextCursor: '' }
    throw new Error(`Unexpected GET ${path}`)
  })
})
afterEach(() => { while (mounted.length) mounted.pop()!.unmount() })

describe('AppPolicyWorkspace', () => {
  it('loads selected global groups and filters the catalog', async () => {
    const wrapper = mountWorkspace(); await flushPromises()
    const boxes = wrapper.findAll('input[type=checkbox]')
    expect(boxes).toHaveLength(2)
    expect((boxes[1]!.element as HTMLInputElement).checked).toBe(true)
    await wrapper.get('input[type=search]').setValue('exceptions')
    expect(wrapper.text()).toContain('Exceptions')
    expect(wrapper.text()).not.toContain('Corporate staff')
  })

  it('replaces the complete selection atomically', async () => {
    put.mockResolvedValue([groups[0]!])
    const wrapper = mountWorkspace(); await flushPromises()
    await wrapper.findAll('input[type=checkbox]')[0]!.setValue(true)
    await wrapper.findAll('input[type=checkbox]')[1]!.setValue(false)
    await wrapper.get('[data-test=save-groups]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith(`${base}/groups`, { groupIds: [10] })
  })

  it('updates the independent access restriction switch', async () => {
    post.mockResolvedValue({ ...workspace.app, accessRestricted: false })
    const wrapper = mountWorkspace(); await flushPromises()
    await wrapper.get('[role=switch]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith(`${base}/access/set-restricted`, { restricted: false })
  })

  it('shows shared group edit links only to administrators', async () => {
    const manager = mountWorkspace('manager'); await flushPromises()
    expect(manager.find('a[href="/admin/groups/21"]').exists()).toBe(false)
    const admin = mountWorkspace('admin'); await flushPromises()
    expect(admin.find('a[href="/admin/groups/21"]').exists()).toBe(true)
  })

  it('resets selected state when the application identity changes', async () => {
    const wrapper = mountWorkspace(); await flushPromises()
    await wrapper.findAll('input[type=checkbox]')[0]!.setValue(true)
    await wrapper.setProps({ kind: 'saml', appId: '44', displayName: 'Docs' })
    expect(wrapper.findAll('input[type=checkbox]').every(box => !(box.element as HTMLInputElement).checked)).toBe(true)
  })
})
