import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { Select } from '@/components/ui/select'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
import AdminAccountsView from './AdminAccountsView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminAccountsView, { global: { plugins: [i18n()] } })

const ACCOUNTS = [
  { id: 1, username: 'alice', displayName: 'Alice Smith', role: 'admin', disabled: false, lastSignInAt: '2026-06-08T00:00:00Z', matchingIdentities: [] },
  { id: 2, username: 'bob', displayName: 'Bob Lee', role: 'user', disabled: true, matchingIdentities: [] },
]
const DESCRIPTORS = [
  {
    slug: 'steam', displayName: 'Steam', protocol: 'steam',
    searchFields: [
      { key: 'steamId', operators: ['exact'] },
      { key: 'personaName', operators: ['exact', 'prefix', 'contains'] },
    ],
  },
  {
    slug: 'vrchat', displayName: 'VRChat', protocol: 'vrchat',
    searchFields: [
      { key: 'userId', operators: ['exact'] },
      { key: 'displayName', operators: ['exact', 'prefix', 'contains'] },
    ],
  },
]
const page = <T>(items: T[], nextCursor = '') => ({ items, nextCursor })

function mockGets(accounts = ACCOUNTS, nextCursor = ''): void {
  get.mockImplementation(async (path: string) =>
    path.startsWith('/api/prohibitorum/identity-providers')
      ? page(DESCRIPTORS)
      : page(accounts, nextCursor))
}

function accountCalls(): string[] {
  return get.mock.calls
    .map(([path]) => String(path))
    .filter((path) => path.startsWith('/api/prohibitorum/accounts'))
}

async function setSelect(wrapper: VueWrapper, index: number, value: string): Promise<void> {
  const select = wrapper.findAllComponents(Select)[index]
  if (!select) throw new Error(`Missing select ${index}`)
  select.vm.$emit('update:modelValue', value)
  await flushPromises()
}

interface AccountPage {
  items: Array<(typeof ACCOUNTS)[number]>
  nextCursor: string
}

function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve!: (value: T) => void

  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

beforeEach(() => {
  get.mockReset()
  push.mockReset()
})

afterEach(() => {
  vi.useRealTimers()
  document.body.innerHTML = ''
})

describe('AdminAccountsView', () => {
  it('loads descriptors and lists accounts with role and state', async () => {
    mockGets()
    const wrapper = mountView()
    await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/accounts')
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/identity-providers?limit=100')
    expect(wrapper.text()).toContain('Alice Smith')
    expect(wrapper.text()).toContain('@bob')
    expect(wrapper.text()).toContain(en.admin.accounts.disabled)
  })

  it('row click and keyboard activation navigate to account detail', async () => {
    mockGets()
    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('[data-test="account-row-2"]').trigger('keydown.enter')
    expect(push).toHaveBeenCalledWith('/admin/accounts/2')
    await wrapper.find('[data-test="account-row-1"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/accounts/1')
  })

  it('invite navigates to invitations', async () => {
    mockGets([])
    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('[data-test="invite"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/invitations')
  })

  it('shows the unfiltered directory empty state with no duplicate CTA', async () => {
    mockGets([])
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain(en.admin.accounts.empty)
    expect(wrapper.find('[data-test="invite"]').exists()).toBe(true)
  })

  it('debounces draft search for 300 ms, commits a trimmed q, and issues one request', async () => {
    vi.useFakeTimers()
    mockGets()
    const wrapper = mountView()
    await flushPromises()
    get.mockClear()

    await wrapper.find('[data-test="accounts-filter"]').setValue('a')
    await vi.advanceTimersByTimeAsync(100)
    await wrapper.find('[data-test="accounts-filter"]').setValue('al')
    await vi.advanceTimersByTimeAsync(100)
    await wrapper.find('[data-test="accounts-filter"]').setValue('  Alice Smith  ')
    await vi.advanceTimersByTimeAsync(299)
    expect(accountCalls()).toEqual([])

    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(accountCalls()).toEqual(['/api/prohibitorum/accounts?q=Alice+Smith'])
  })

  it('clears the pending search timer when unmounted', async () => {
    vi.useFakeTimers()
    mockGets()
    const wrapper = mountView()
    await flushPromises()
    get.mockClear()
    await wrapper.find('[data-test="accounts-filter"]').setValue('alice')
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(300)
    expect(accountCalls()).toEqual([])
  })

  it('suppresses a stale search response after a newer committed query wins', async () => {
    vi.useFakeTimers()
    const first = deferred<AccountPage>()
    const second = deferred<AccountPage>()
    get.mockImplementation(async (path: string) => {
      if (path.startsWith('/api/prohibitorum/identity-providers')) return page(DESCRIPTORS)
      if (path.includes('q=first')) return first.promise
      if (path.includes('q=second')) return second.promise
      return page(ACCOUNTS)
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('[data-test="accounts-filter"]').setValue('first')
    await vi.advanceTimersByTimeAsync(300)
    await wrapper.find('[data-test="accounts-filter"]').setValue('second')
    await vi.advanceTimersByTimeAsync(300)

    second.resolve(page([ACCOUNTS[1]!]))
    await flushPromises()
    expect(wrapper.text()).toContain('Bob Lee')
    expect(wrapper.text()).not.toContain('Alice Smith')

    first.resolve(page([ACCOUNTS[0]!]))
    await flushPromises()
    expect(wrapper.text()).toContain('Bob Lee')
    expect(wrapper.text()).not.toContain('Alice Smith')
  })

  it('resets cursor history and reloads page one when q changes', async () => {
    vi.useFakeTimers()
    get.mockImplementation(async (path: string) => {
      if (path.startsWith('/api/prohibitorum/identity-providers')) return page(DESCRIPTORS)
      if (path.includes('q=alice')) return page([ACCOUNTS[0]!])
      if (path.includes('cursor=next-1')) return page([ACCOUNTS[1]!])
      return page([ACCOUNTS[0]!], 'next-1')
    })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('[data-test="next-page"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="page-indicator"]').text()).toContain('2')

    await wrapper.find('[data-test="accounts-filter"]').setValue('alice')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(accountCalls().at(-1)).toBe('/api/prohibitorum/accounts?q=alice')
    expect(wrapper.find('[data-test="page-indicator"]').text()).toContain('1')
  })

  it('encodes only a complete descriptor-approved advanced query', async () => {
    mockGets()
    const wrapper = mountView()
    await flushPromises()
    await setSelect(wrapper, 0, 'vrchat')
    get.mockClear()
    await setSelect(wrapper, 1, 'displayName')
    await setSelect(wrapper, 2, 'contains')
    expect(accountCalls()).toEqual([])

    await wrapper.find('[data-test="accounts-advanced-value"]').setValue('  A&B / 星  ')
    await flushPromises()
    expect(accountCalls()).toEqual([
      '/api/prohibitorum/accounts?provider=vrchat&field=displayName&value=A%26B+%2F+%E6%98%9F&match=contains',
    ])
  })

  it('shows fields and operators only from the selected provider descriptor', async () => {
    mockGets()
    const wrapper = mountView()
    await flushPromises()
    await setSelect(wrapper, 0, 'vrchat')
    const vm = wrapper.vm as unknown as {
      descriptorFields: Array<{ key: string; operators: string[] }>
      selectedField?: { key: string; operators: string[] }
    }
    expect(vm.descriptorFields.map((field) => field.key)).toEqual(['userId', 'displayName'])
    expect(vm.descriptorFields.map((field) => field.key)).not.toContain('personaName')

    await setSelect(wrapper, 1, 'userId')
    expect(vm.selectedField?.operators).toEqual(['exact'])
    expect(vm.selectedField?.operators).not.toContain('prefix')
    expect(vm.selectedField?.operators).not.toContain('contains')
  })

  it('issues a valid provider-only filter and clears invalid field state when provider changes', async () => {
    mockGets()
    const wrapper = mountView()
    await flushPromises()
    await setSelect(wrapper, 0, 'vrchat')
    await setSelect(wrapper, 1, 'displayName')
    await setSelect(wrapper, 2, 'contains')
    await wrapper.find('[data-test="accounts-advanced-value"]').setValue('avi')
    await flushPromises()
    get.mockClear()

    await setSelect(wrapper, 0, 'steam')
    expect(accountCalls()).toEqual(['/api/prohibitorum/accounts?provider=steam'])
    expect(wrapper.find('[data-test="accounts-advanced-value"]').exists()).toBe(false)
    const vm = wrapper.vm as unknown as { fieldKey: string; matchOperator: string; filterValue: string }
    expect({ field: vm.fieldKey, match: vm.matchOperator, value: vm.filterValue }).toEqual({ field: '', match: '', value: '' })
  })

  it('clearing all filters restores unfiltered first-page results', async () => {
    vi.useFakeTimers()
    get.mockImplementation(async (path: string) => {
      if (path.startsWith('/api/prohibitorum/identity-providers')) return page(DESCRIPTORS)
      return path === '/api/prohibitorum/accounts' ? page(ACCOUNTS) : page([ACCOUNTS[1]!])
    })
    const wrapper = mountView()
    await flushPromises()
    await setSelect(wrapper, 0, 'vrchat')
    await wrapper.find('[data-test="accounts-filter"]').setValue('bob')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(wrapper.text()).not.toContain('Alice Smith')
    get.mockClear()

    await wrapper.find('[data-test="accounts-clear"]').trigger('click')
    await flushPromises()
    expect(accountCalls()).toEqual(['/api/prohibitorum/accounts'])
    expect(wrapper.text()).toContain('Alice Smith')
    expect(wrapper.find('[data-test="page-indicator"]').text()).toContain('1')
  })

  it('renders only API matching identities with semantic provider context', async () => {
    vi.useFakeTimers()
    const matched = {
      ...ACCOUNTS[0]!,
      matchingIdentities: [{
        id: 31,
        providerSlug: 'vrchat',
        providerDisplayName: 'VRChat Community',
        protocol: 'vrchat',
        subject: 'usr_1234567890abcdefgh',
        email: 'avatar@example.com',
        data: { displayName: 'Avi Star', profileUrl: 'https://vrchat.com/home/user/usr_1234567890abcdefgh', secretNote: 'never render me' },
        linkedAt: '2026-07-10T00:00:00Z',
      }],
      identities: [{ providerDisplayName: 'Unrelated Steam', subject: '7656119' }],
    }
    get.mockImplementation(async (path: string) => {
      if (path.startsWith('/api/prohibitorum/identity-providers')) return page(DESCRIPTORS)
      return path.includes('q=avi') ? page([matched]) : page(ACCOUNTS)
    })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('[data-test="accounts-filter"]').setValue('avi')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    expect(wrapper.text()).toContain('VRChat Community')
    expect(wrapper.text()).toContain(en.identity.vrchatUserId)
    expect(wrapper.text()).toContain('usr_1234567890abcdefgh')
    expect(wrapper.text()).toContain(en.identity.displayName)
    expect(wrapper.text()).toContain('Avi Star')
    expect(wrapper.text()).not.toContain('secretNote')
    expect(wrapper.text()).not.toContain('never render me')
    expect(wrapper.text()).not.toContain('Unrelated Steam')
  })

  it('describes a filtered empty result as no match in the complete directory', async () => {
    vi.useFakeTimers()
    get.mockImplementation(async (path: string) => {
      if (path.startsWith('/api/prohibitorum/identity-providers')) return page(DESCRIPTORS)
      return path.includes('q=missing') ? page([]) : page(ACCOUNTS)
    })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('[data-test="accounts-filter"]').setValue('missing')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(wrapper.find('[data-test="accounts-no-matches"]').text()).toBe(en.admin.accounts.noMatches)
    expect(en.admin.accounts.noMatches).toContain('directory')
  })

  it('surfaces an app load error inline but leaves server errors to the global toast', async () => {
    get.mockRejectedValue({ code: 'forbidden', message: 'zh' })
    const forbidden = mountView()
    await flushPromises()
    expect(forbidden.text()).toContain(en.errors.codes.forbidden)
    forbidden.unmount()

    get.mockReset()
    get.mockRejectedValue({ code: 'server_error', message: 'boom' })
    const server = mountView()
    await flushPromises()
    expect(server.text()).not.toContain(en.errors.codes.server_error)
  })

  it('shows the admin diagnostic action when an error has a request ID', async () => {
    get.mockRejectedValue({ code: 'forbidden', requestId: 'rid-adm-1' })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-test="error-diagnostic"]').exists()).toBe(true)
  })
})
