import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
vi.mock('@/lib/sudo', () => ({ withSudo: (operation: () => Promise<unknown>) => operation() }))
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: '1' } }),
  useRouter: () => ({ push }),
}))

import { api } from '@/lib/api'
import AdminGroupDetailView from './AdminGroupDetailView.vue'

const get = vi.mocked(api.get)
const group = { id: 1, kind: 'manual', slug: 'members', displayName: 'Members', description: 'People with access', exposedToDownstream: true }
const applications = [
  { kind: 'oidc', appId: 'wiki', displayName: 'Wiki', iconUrl: '/icon/oidc_client/wiki?v=abcdef01' },
  { kind: 'saml', appId: '7', displayName: 'SAML Portal' },
]

function mountView() {
  return mount(AdminGroupDetailView, {
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })],
      stubs: { RouterLink: { template: '<a><slot /></a>' } },
    },
    attachTo: document.body,
  })
}

beforeEach(() => {
  get.mockReset()
  push.mockReset()
  get.mockImplementation(async (path: string) => {
    if (path === '/api/prohibitorum/groups/1') return group
    if (path === '/api/prohibitorum/groups/1/applications') return { items: applications, nextCursor: '' }
    if (path === '/api/prohibitorum/groups/providers') return []
    if (path === '/api/prohibitorum/groups/1/decisions?limit=100') return { items: [], nextCursor: '' }
    throw new Error('Unexpected GET ' + path)
  })
})

describe('AdminGroupDetailView', () => {
  it('places type and exposure beside slug and ID under the title', async () => {
    const wrapper = mountView()
    await flushPromises()
    const heading = wrapper.get('h1')
    const metadata = heading.element.nextElementSibling
    expect(heading.text()).toBe('Members')
    expect(metadata?.textContent).toContain('members · #1')
    expect(metadata?.textContent).toContain('Manual')
    expect(metadata?.textContent).toContain('Included in downstream groups')
  })

  it('renders linked applications last as responsive icon cards', async () => {
    const wrapper = mountView()
    await flushPromises()
    const cards = wrapper.findAll('[data-slot="card"]')
    expect(cards.at(-1)?.text()).toContain('Used by applications')
    const applicationsSection = wrapper.get('[data-test="group-applications"]')
    expect(applicationsSection.classes()).toContain('sm:grid-cols-2')
    expect(applicationsSection.text()).toContain('Wiki')
    expect(applicationsSection.text()).toContain('SAML Portal')
    expect(applicationsSection.get('img').attributes('src')).toBe('/icon/oidc_client/wiki?v=abcdef01')
  })

  it('does not preload the entire account directory', async () => {
    mountView()
    await flushPromises()
    expect(get.mock.calls.map(([path]) => String(path)).some(path => path.startsWith('/api/prohibitorum/accounts?'))).toBe(false)
  })

  it('loads every decision page before excluding decided accounts from search', async () => {
    get.mockImplementation(async (path: string) => {
      if (path === '/api/prohibitorum/groups/1') return group
      if (path === '/api/prohibitorum/groups/1/applications') return { items: applications, nextCursor: '' }
      if (path === '/api/prohibitorum/groups/providers') return []
      if (path === '/api/prohibitorum/groups/1/decisions?limit=100') {
        return {
          items: [{ account: { id: 101, username: 'alice', displayName: 'Alice Avery' }, effect: 'allow', updatedAt: '2026-07-25T10:00:00Z' }],
          nextCursor: 'next/page',
        }
      }
      if (path === '/api/prohibitorum/groups/1/decisions?limit=100&cursor=next%2Fpage') {
        return {
          items: [{ account: { id: 102, username: 'bob', displayName: 'Bob Baker' }, effect: 'deny', updatedAt: '2026-07-25T11:00:00Z' }],
          nextCursor: '',
        }
      }
      if (path === '/api/prohibitorum/accounts?q=engineer&limit=100') {
        return {
          items: [
            { id: 101, username: 'alice', displayName: 'Alice Avery', disabled: false },
            { id: 102, username: 'bob', displayName: 'Bob Baker', disabled: false },
            { id: 103, username: 'carol', displayName: 'Carol Chen', disabled: false },
          ],
          nextCursor: '',
        }
      }
      throw new Error('Unexpected GET ' + path)
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="manual-account-search"]').setValue('engineer')
    await wrapper.get('form[role="search"]').trigger('submit')
    await flushPromises()

    expect(wrapper.find('[data-test="manual-account-result-101"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="manual-account-result-102"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="manual-account-result-103"]').text()).toContain('Carol Chen')
  })
})
