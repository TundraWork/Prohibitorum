import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }))
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))

import { api } from '@/lib/api'
import AdminGroupsView from './AdminGroupsView.vue'

const get = vi.mocked(api.get)
const groups = [
  { id: 1, kind: 'manual', slug: 'members', displayName: 'Members', description: 'People with access', exposedToDownstream: true, applicationCount: 3 },
  { id: 2, kind: 'rule', slug: 'verified', displayName: 'Verified', exposedToDownstream: false, applicationCount: 0, rule: { version: 1, condition: { fact: 'login_method', method: 'passkey' } } },
]

function mountView() {
  return mount(AdminGroupsView, {
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })],
    },
    attachTo: document.body,
  })
}

beforeEach(() => {
  get.mockReset()
  push.mockReset()
  get.mockImplementation(async (path: string) => {
    if (path === '/api/prohibitorum/groups') return groups
    if (path === '/api/prohibitorum/groups/providers') return []
    throw new Error('Unexpected GET ' + path)
  })
})

describe('AdminGroupsView', () => {
  it('renders one group table with metadata, descriptions, and application counts', async () => {
    const wrapper = mountView()
    await flushPromises()

    const table = wrapper.get('[data-test="groups-table"]')
    expect(table.find('table').exists()).toBe(true)
    expect(table.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.get('[data-test="group-row-1"]').text()).toContain('members · #1')
    expect(wrapper.get('[data-test="group-row-1"]').text()).toContain('People with access')
    expect(wrapper.get('[data-test="group-row-1"]').text()).toContain('3')
    expect(wrapper.get('[data-test="group-row-2"]').text()).toContain('No description')
  })

  it('opens a group with pointer, Enter, and Space activation', async () => {
    const wrapper = mountView()
    await flushPromises()
    const row = wrapper.get('[data-test="group-row-1"]')
    expect(row.attributes('tabindex')).toBe('0')
    expect(row.attributes('aria-label')).toBe('Open Members')

    await row.trigger('click')
    await row.trigger('keydown', { key: 'Enter' })
    await row.trigger('keydown', { key: ' ' })
    expect(push).toHaveBeenCalledTimes(3)
    expect(push).toHaveBeenLastCalledWith('/admin/groups/1')
  })

  it('keeps name, slug, ID, and type filtering', async () => {
    const wrapper = mountView()
    await flushPromises()
    const search = wrapper.get<HTMLInputElement>('input[type="search"]')
    await search.setValue('2')
    expect(wrapper.find('[data-test="group-row-1"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="group-row-2"]').exists()).toBe(true)

    await search.setValue('')
    await wrapper.get('select').setValue('manual')
    expect(wrapper.find('[data-test="group-row-1"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="group-row-2"]').exists()).toBe(false)
  })
})
