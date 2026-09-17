import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'

const { withSudo } = vi.hoisted(() => ({
  withSudo: vi.fn((operation: () => Promise<unknown>) => operation()),
}))

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }))
vi.mock('@/lib/sudo', () => ({ withSudo }))

import { api } from '@/lib/api'
import AppManagerCard from './AppManagerCard.vue'

const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

type AppKind = 'oidc' | 'forward_auth' | 'saml'

interface AppManagerView {
  id: number
  username: string
  displayName: string
  disabled: boolean
  assignedAt: string
}

interface AccountView {
  id: number
  username: string
  displayName: string
}

interface AccountPage {
  items: AccountView[]
  nextCursor: string
}

const APP_ID = 'client/alpha'
const MANAGERS_ENDPOINT = '/api/prohibitorum/oidc-applications/client%2Falpha/managers'

const ACTIVE_MANAGER: AppManagerView = {
  id: 7,
  username: 'grace',
  displayName: 'Grace Hopper',
  disabled: false,
  assignedAt: '2026-07-25T11:30:00Z',
}

const DISABLED_MANAGER: AppManagerView = {
  id: 14,
  username: 'linus',
  displayName: 'Linus Torvalds',
  disabled: true,
  assignedAt: '2026-07-24T09:00:00Z',
}

const MANAGERS = [ACTIVE_MANAGER, DISABLED_MANAGER]

const ACTIVE_CANDIDATE: AccountView = { id: 8, username: 'hedy', displayName: 'Hedy Lamarr' }

const CANDIDATE_PAGE: AccountPage = {
  items: [ACTIVE_CANDIDATE],
  nextCursor: '',
}

type ManagerResult = AppManagerView[] | (() => AppManagerView[])

function mountCard(
  kind: AppKind = 'oidc',
  appId = APP_ID,
  props: { mode?: 'admin' | 'manager'; currentAccountId?: number } = {},
) {
  return mount(AppManagerCard, {
    props: { kind, appId, ...props },
    global: {
      plugins: [createI18n({
        legacy: false,
        locale: 'en',
        fallbackLocale: 'en',
        messages: { en },
      })],
    },
    attachTo: document.body,
  })
}

function mockGets(
  managers: ManagerResult = MANAGERS,
  accounts: AccountPage = CANDIDATE_PAGE,
): void {
  get.mockImplementation(async (path: string) => {
    if (path.endsWith('/managers')) {
      return typeof managers === 'function' ? managers() : managers
    }
    if (path.startsWith('/api/prohibitorum/managed-applications/manager-candidates')) return accounts
    throw new Error(`Unexpected GET ${path}`)
  })
}

function managerGetCalls(): string[] {
  return get.mock.calls
    .map(([path]) => String(path))
    .filter((path) => path.endsWith('/managers'))
}


async function searchCandidates(wrapper: VueWrapper, query: string): Promise<void> {
  const search = wrapper.get<HTMLInputElement>('[data-test="manager-account-search"]')
  await search.setValue(query)
  await search.trigger('keydown', { key: 'Enter' })
  await vi.runAllTimersAsync()
  await flushPromises()
}

beforeEach(() => {
  get.mockReset()
  post.mockReset()
  withSudo.mockReset()
  withSudo.mockImplementation((operation: () => Promise<unknown>) => operation())
  vi.useFakeTimers()
  document.body.innerHTML = ''
})

afterEach(() => {
  vi.useRealTimers()
  document.body.innerHTML = ''
})

describe('AppManagerCard', () => {
  it('renders a bare manager array with assigned-at and disabled manager state', async () => {
    mockGets()
    const wrapper = mountCard()
    await flushPromises()

    expect(managerGetCalls()).toEqual([MANAGERS_ENDPOINT])
    expect(withSudo).not.toHaveBeenCalled()

    const activeRow = wrapper.get('[data-test="manager-row-7"]')
    expect(activeRow.text()).toContain('Grace Hopper')
    expect(activeRow.text()).toContain('grace')
    expect(activeRow.get('time').attributes('datetime')).toBe(ACTIVE_MANAGER.assignedAt)
    expect(activeRow.get('time').text()).not.toBe('')

    const disabledRow = wrapper.get('[data-test="manager-row-14"]')
    expect(disabledRow.text()).toContain('Linus Torvalds')
    expect(disabledRow.text()).toContain(en.admin.account.disabledLabel)
    expect(wrapper.get<HTMLButtonElement>('[data-test="manager-remove-14"]').element.disabled).toBe(false)
  })

  it.each([
    {
      kind: 'oidc' as const,
      appId: 'oidc/client & one',
      endpoint: '/api/prohibitorum/oidc-applications/oidc%2Fclient%20%26%20one/managers',
    },
    {
      kind: 'forward_auth' as const,
      appId: 'forward/auth & two',
      endpoint: '/api/prohibitorum/forward-auth-apps/forward%2Fauth%20%26%20two/managers',
    },
    {
      kind: 'saml' as const,
      appId: 'saml/provider & three',
      endpoint: '/api/prohibitorum/saml-applications/saml%2Fprovider%20%26%20three/managers',
    },
  ])('uses the encoded $kind manager endpoint', async ({ kind, appId, endpoint }) => {
    mockGets([])
    mountCard(kind, appId)
    await flushPromises()

    expect(managerGetCalls()).toEqual([endpoint])
    expect(withSudo).not.toHaveBeenCalled()
  })

  it('searches candidate accounts with q rather than sudo-wrapping a read', async () => {
    mockGets()
    const wrapper = mountCard()
    await flushPromises()

    await searchCandidates(wrapper, ' Ada Lovelace ')

    expect(
      get.mock.calls
        .map(([path]) => String(path))
        .filter((path) => path.startsWith('/api/prohibitorum/managed-applications/manager-candidates')),
    ).toEqual(['/api/prohibitorum/managed-applications/manager-candidates?q=Ada%20Lovelace'])
    expect(withSudo).not.toHaveBeenCalled()
  })

  it('discards in-flight results when the search text changes', async () => {
    let resolveAccounts!: (page: AccountPage) => void
    const accountsResponse = new Promise<AccountPage>((resolve) => {
      resolveAccounts = resolve
    })
    get.mockImplementation(async (path: string) => {
      if (path.endsWith('/managers')) return []
      if (path.startsWith('/api/prohibitorum/managed-applications/manager-candidates')) return accountsResponse
      throw new Error(`Unexpected GET ${path}`)
    })

    const wrapper = mountCard()
    await flushPromises()
    const search = wrapper.get<HTMLInputElement>('[data-test="manager-account-search"]')
    await search.setValue('Hedy')
    await search.trigger('keydown', { key: 'Enter' })
    await search.setValue('Ada')

    resolveAccounts({ items: [ACTIVE_CANDIDATE], nextCursor: '' })
    await flushPromises()

    expect(wrapper.find('[data-test="manager-account-result-8"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Hedy Lamarr')
  })

  it('renders the safe active-manager candidates returned by the scoped endpoint', async () => {
    mockGets()
    const wrapper = mountCard()
    await flushPromises()

    await searchCandidates(wrapper, 'manager')

    expect(wrapper.find('[data-test="manager-account-result-8"]').exists()).toBe(true)
    expect(wrapper.get<HTMLButtonElement>('[data-test="manager-assign-8"]').element.disabled).toBe(false)
  })

  it('assigns one active app manager through sudo and refreshes the manager list once', async () => {
    let assigned = false
    mockGets(() => assigned ? [...MANAGERS, { ...ACTIVE_CANDIDATE, assignedAt: '2026-07-26T10:00:00Z' }] : MANAGERS)
    post.mockImplementation(async (path: string) => {
      expect(path).toBe(MANAGERS_ENDPOINT)
      assigned = true
      return {}
    })

    const wrapper = mountCard()
    await flushPromises()
    await searchCandidates(wrapper, 'Hedy')

    await wrapper.get('[data-test="manager-assign-8"]').trigger('click')
    await flushPromises()

    expect(withSudo).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith(MANAGERS_ENDPOINT, { accountId: 8 })
    expect(managerGetCalls()).toEqual([MANAGERS_ENDPOINT, MANAGERS_ENDPOINT])
    expect(wrapper.find('[data-test="manager-row-8"]').exists()).toBe(true)
  })

  it('removes one manager through sudo and refreshes the manager list once', async () => {
    let removed = false
    mockGets(() => removed ? [DISABLED_MANAGER] : MANAGERS)
    post.mockImplementation(async (path: string) => {
      expect(path).toBe(`${MANAGERS_ENDPOINT}/remove`)
      removed = true
      return {}
    })

    const wrapper = mountCard()
    await flushPromises()

    await wrapper.get('[data-test="manager-remove-7"]').trigger('click')
    await flushPromises()

    expect(withSudo).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith(`${MANAGERS_ENDPOINT}/remove`, { accountId: 7 })
    expect(managerGetCalls()).toEqual([MANAGERS_ENDPOINT, MANAGERS_ENDPOINT])
    expect(wrapper.find('[data-test="manager-row-7"]').exists()).toBe(false)
  })

  it('emits self-removed without refetching inaccessible data in manager mode', async () => {
    mockGets()
    post.mockResolvedValue({})
    const wrapper = mountCard('oidc', APP_ID, { mode: 'manager', currentAccountId: ACTIVE_MANAGER.id })
    await flushPromises()

    await wrapper.get('[data-test="manager-remove-7"]').trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(`${MANAGERS_ENDPOINT}/remove`, { accountId: ACTIVE_MANAGER.id })
    expect(wrapper.emitted('self-removed')).toEqual([[]])
    expect(managerGetCalls()).toEqual([MANAGERS_ENDPOINT])
  })

  it('does not invoke the assignment mutation when sudo is cancelled', async () => {
    mockGets()
    withSudo.mockResolvedValueOnce(undefined)
    const wrapper = mountCard()
    await flushPromises()
    await searchCandidates(wrapper, 'Hedy')

    await wrapper.get('[data-test="manager-assign-8"]').trigger('click')
    await flushPromises()

    expect(withSudo).toHaveBeenCalledTimes(1)
    expect(post).not.toHaveBeenCalled()
    expect(managerGetCalls()).toEqual([MANAGERS_ENDPOINT])
  })

  it('keeps a server error visible and lets the same assignment control retry successfully', async () => {
    let assigned = false
    mockGets(() => assigned ? [...MANAGERS, { ...ACTIVE_CANDIDATE, assignedAt: '2026-07-26T10:00:00Z' }] : MANAGERS)
    post
      .mockRejectedValueOnce({ code: 'server_error' })
      .mockImplementationOnce(async () => {
        assigned = true
        return {}
      })

    const wrapper = mountCard()
    await flushPromises()
    await searchCandidates(wrapper, 'Hedy')

    const assign = wrapper.get<HTMLButtonElement>('[data-test="manager-assign-8"]')
    await assign.trigger('click')
    await flushPromises()

    expect(wrapper.get('[role="alert"]').text()).toContain(en.errors.unknown)
    expect(assign.element.disabled).toBe(false)

    await assign.trigger('click')
    await flushPromises()

    expect(withSudo).toHaveBeenCalledTimes(2)
    expect(post).toHaveBeenCalledTimes(2)
    expect(managerGetCalls()).toEqual([MANAGERS_ENDPOINT, MANAGERS_ENDPOINT])
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="manager-row-8"]').exists()).toBe(true)
  })

  it('uses focusable native account controls with account-specific accessible labels', async () => {
    mockGets()
    const wrapper = mountCard()
    await flushPromises()
    await searchCandidates(wrapper, 'Hedy')

    const search = wrapper.get<HTMLInputElement>('[data-test="manager-account-search"]')
    expect(search.element.tagName).toBe('INPUT')
    expect(search.attributes('type')).toBe('search')
    expect(search.attributes('aria-label')).toBeTruthy()
    search.element.focus()
    expect(document.activeElement).toBe(search.element)

    const assign = wrapper.get<HTMLButtonElement>('[data-test="manager-assign-8"]')
    const remove = wrapper.get<HTMLButtonElement>('[data-test="manager-remove-7"]')
    for (const control of [assign, remove]) {
      expect(control.element.tagName).toBe('BUTTON')
      expect(control.attributes('type')).toBe('button')
      control.element.focus()
      expect(document.activeElement).toBe(control.element)
    }

    expect(assign.attributes('aria-label')).toContain('Hedy Lamarr')
    expect(remove.attributes('aria-label')).toContain('Grace Hopper')
  })
})
