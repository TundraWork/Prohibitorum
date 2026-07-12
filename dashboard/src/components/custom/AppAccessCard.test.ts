import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post)
import AppAccessCard from './AppAccessCard.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountCard = (kind: 'oidc' | 'saml' = 'oidc', appId = 'my-app') =>
  mount(AppAccessCard, {
    props: { kind, appId },
    global: {
      plugins: [i18n()],
      stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } },
    },
    attachTo: document.body,
  })

const ACCESS_RESTRICTED = {
  accessRestricted: true,
  groups: { items: [{ id: 10, slug: 'eng', displayName: 'Engineering' }], nextCursor: '' },
      accounts: { items: [{ id: 7, username: 'carol', displayName: 'Carol Ng' }], nextCursor: '' },
}
const ACCESS_OPEN = {
  accessRestricted: false,
  groups: { items: [], nextCursor: '' },
      accounts: { items: [], nextCursor: '' },
}
const ALL_GROUPS = [
  { id: 10, slug: 'eng', displayName: 'Engineering' },
  { id: 20, slug: 'ops', displayName: 'Operations' },
]
const ALL_ACCOUNTS = [
  { id: 7, username: 'carol', displayName: 'Carol Ng' },
  { id: 8, username: 'bob', displayName: 'Bob Smith' },
]

// Mock GET router: ${base}/access, /groups, /accounts
function mockGets(
  access = ACCESS_RESTRICTED,
  groups = ALL_GROUPS,
  accounts = ALL_ACCOUNTS
) {
  get.mockImplementation(async (p: string) => {
    if (String(p).endsWith('/access')) return access
    if (String(p) === '/api/prohibitorum/groups') return groups
    if (String(p) === '/api/prohibitorum/accounts') return accounts
    return {}
  })
}

// ConfirmDialog destructive confirm button helper (teleported to body)
function clickConfirm(label: string) {
  const btns = Array.from(document.body.querySelectorAll('button'))
    .filter((b) => b.getAttribute('data-variant') === 'destructive' && b.textContent?.includes(label))
  btns[btns.length - 1]!.click()
}

beforeEach(() => { get.mockReset(); post.mockReset() })

describe('AppAccessCard', () => {
  // ─── Three separate useApi() instances / race prevention ───────────────────

  it('issues three separate GET calls on mount — access, groups, and accounts all populate', async () => {
    mockGets()
    const w = mountCard('oidc', 'client-abc')
    await flushPromises()

    // Verify all three fetches happened
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/oidc-applications/client-abc/access')
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/groups')
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/accounts')

    // Verify data actually populated (not silently no-op'd by busy-guard race)
    const vm = w.vm as unknown as {
      assignedGroups: Array<{ id: number }>
      assignedAccounts: Array<{ id: number }>
      allGroups: Array<{ id: number }>
      allAccounts: Array<{ id: number }>
    }
    expect(vm.assignedGroups).toHaveLength(1)
    expect(vm.assignedAccounts).toHaveLength(1)
    expect(vm.allGroups).toHaveLength(2)
    expect(vm.allAccounts).toHaveLength(2)
  })

  // ─── Pickers POPULATE and addable computed is correct ──────────────────────

  it('addableGroups excludes already-assigned groups and populates with the rest', async () => {
    // ACCESS_RESTRICTED has group 10 assigned; ALL_GROUPS has 10 and 20
    mockGets()
    const w = mountCard('oidc', 'client-abc')
    await flushPromises()

    const vm = w.vm as unknown as {
      addableGroups: Array<{ id: number; displayName: string }>
    }
    // allGroups has 2 entries; group 10 is already assigned → addable must be only group 20
    expect(vm.addableGroups).toHaveLength(1)
    expect(vm.addableGroups[0].id).toBe(20)
    expect(vm.addableGroups[0].displayName).toBe('Operations')
  })

  it('addableAccounts excludes already-assigned accounts and populates with the rest', async () => {
    // ACCESS_RESTRICTED has account 7 assigned; ALL_ACCOUNTS has 7 and 8
    mockGets()
    const w = mountCard('oidc', 'client-abc')
    await flushPromises()

    const vm = w.vm as unknown as {
      addableAccounts: Array<{ id: number; displayName: string }>
    }
    // allAccounts has 2 entries; account 7 is already assigned → addable must be only account 8
    expect(vm.addableAccounts).toHaveLength(1)
    expect(vm.addableAccounts[0].id).toBe(8)
    expect(vm.addableAccounts[0].displayName).toBe('Bob Smith')
  })

  it('renders assigned group and account rows after mount', async () => {
    mockGets()
    const w = mountCard('oidc', 'client-abc')
    await flushPromises()

    // Group row for id=10
    expect(w.find('[data-test="group-row-10"]').exists()).toBe(true)
    expect(w.text()).toContain('Engineering')

    // Account row for id=7
    expect(w.find('[data-test="account-row-7"]').exists()).toBe(true)
    expect(w.text()).toContain('Carol Ng')
  })

  // ─── OIDC vs SAML base path ─────────────────────────────────────────────────

  it('uses oidc-applications base path when kind=oidc', async () => {
    mockGets()
    mountCard('oidc', 'my-client')
    await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/oidc-applications/my-client/access')
  })

  it('uses saml-applications base path when kind=saml', async () => {
    mockGets()
    mountCard('saml', '42')
    await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/saml-applications/42/access')
  })

  // ─── Restrict toggle ────────────────────────────────────────────────────────

  it('shows inactive hint when accessRestricted is false', async () => {
    mockGets(ACCESS_OPEN)
    const w = mountCard('oidc', 'client-abc')
    await flushPromises()
    expect(w.find('[data-test="access-inactive-hint"]').exists()).toBe(true)
    expect(w.text()).toContain(en.admin.access.inactiveHint)
  })

  it('hides inactive hint when accessRestricted is true', async () => {
    mockGets(ACCESS_RESTRICTED)
    const w = mountCard('oidc', 'client-abc')
    await flushPromises()
    expect(w.find('[data-test="access-inactive-hint"]').exists()).toBe(false)
  })

  it('toggling the switch POSTs set-restricted and refetches access', async () => {
    mockGets()
    post.mockResolvedValue({})
    // After toggle, return the open state on refetch
    let accessCallCount = 0
    get.mockImplementation(async (p: string) => {
      if (String(p).endsWith('/access')) {
        accessCallCount++
        return accessCallCount === 1 ? ACCESS_RESTRICTED : ACCESS_OPEN
      }
      if (String(p) === '/api/prohibitorum/groups') return ALL_GROUPS
      if (String(p) === '/api/prohibitorum/accounts') return ALL_ACCOUNTS
      return {}
    })
    const w = mountCard('oidc', 'client-abc')
    await flushPromises()
    const accessBefore = accessCallCount
    await w.find('[data-test="access-restricted-toggle"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith(
      '/api/prohibitorum/oidc-applications/client-abc/access/set-restricted',
      { restricted: false }
    )
    expect(accessCallCount).toBeGreaterThan(accessBefore)
  })

  // ─── Grant group ────────────────────────────────────────────────────────────

  it('add-group button is disabled when no group is selected', async () => {
    mockGets()
    const w = mountCard('oidc', 'client-abc')
    await flushPromises()
    const btn = w.find<HTMLButtonElement>('[data-test="add-group"]')
    expect(btn.element.disabled).toBe(true)
  })

  it('selecting a group via vm and clicking Add posts grant and refetches', async () => {
    mockGets()
    post.mockResolvedValue({})
    let accessCallCount = 0
    get.mockImplementation(async (p: string) => {
      if (String(p).endsWith('/access')) {
        accessCallCount++
        if (accessCallCount === 1) return { ...ACCESS_RESTRICTED, groups: { items: ACCESS_RESTRICTED.groups?.items ?? ACCESS_RESTRICTED.groups ?? [], nextCursor: '' }, accounts: { items: ACCESS_RESTRICTED.accounts?.items ?? ACCESS_RESTRICTED.accounts ?? [], nextCursor: '' } }
        return {
          accessRestricted: true,
          groups: [
            { id: 10, slug: 'eng', displayName: 'Engineering' },
            { id: 20, slug: 'ops', displayName: 'Operations' },
          ],
          accounts: ACCESS_RESTRICTED.accounts,
        }
      }
      if (String(p) === '/api/prohibitorum/groups') return ALL_GROUPS
      if (String(p) === '/api/prohibitorum/accounts') return ALL_ACCOUNTS
      return {}
    })
    const w = mountCard('oidc', 'client-abc')
    await flushPromises()
    const accessBefore = accessCallCount
    // Drive selectedGroupId via vm (Reka Select not directly settable)
    const vm = w.vm as unknown as { selectedGroupId: string }
    vm.selectedGroupId = '20'
    await flushPromises()
    await w.find('[data-test="add-group"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith(
      '/api/prohibitorum/oidc-applications/client-abc/access/grant',
      { principalKind: 'group', principalId: 20 }
    )
    expect(accessCallCount).toBeGreaterThan(accessBefore)
  })

  // ─── Grant account ──────────────────────────────────────────────────────────

  it('add-account button is disabled when no account is selected', async () => {
    mockGets()
    const w = mountCard('oidc', 'client-abc')
    await flushPromises()
    const btn = w.find<HTMLButtonElement>('[data-test="add-account"]')
    expect(btn.element.disabled).toBe(true)
  })

  it('selecting an account via vm and clicking Add posts grant and refetches', async () => {
    mockGets()
    post.mockResolvedValue({})
    const w = mountCard('oidc', 'client-abc')
    await flushPromises()
    const vm = w.vm as unknown as { selectedAccountId: string }
    vm.selectedAccountId = '8'
    await flushPromises()
    await w.find('[data-test="add-account"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith(
      '/api/prohibitorum/oidc-applications/client-abc/access/grant',
      { principalKind: 'account', principalId: 8 }
    )
  })

  // ─── Revoke group ────────────────────────────────────────────────────────────

  it('clicking remove on a group row opens the confirm dialog and revokes on confirm', async () => {
    mockGets()
    post.mockResolvedValue({})
    const w = mountCard('oidc', 'client-abc')
    await flushPromises()
    await w.find('[data-test="group-remove-10"]').trigger('click')
    await flushPromises()
    clickConfirm(en.admin.access.remove)
    await flushPromises()
    expect(post).toHaveBeenCalledWith(
      '/api/prohibitorum/oidc-applications/client-abc/access/revoke',
      { principalKind: 'group', principalId: 10 }
    )
  })

  // ─── Revoke account ──────────────────────────────────────────────────────────

  it('clicking remove on an account row opens the confirm dialog and revokes on confirm', async () => {
    mockGets()
    post.mockResolvedValue({})
    const w = mountCard('oidc', 'client-abc')
    await flushPromises()
    await w.find('[data-test="account-remove-7"]').trigger('click')
    await flushPromises()
    clickConfirm(en.admin.access.remove)
    await flushPromises()
    expect(post).toHaveBeenCalledWith(
      '/api/prohibitorum/oidc-applications/client-abc/access/revoke',
      { principalKind: 'account', principalId: 7 }
    )
  })

  // ─── Empty states ────────────────────────────────────────────────────────────

  it('shows empty state when no groups or accounts are assigned', async () => {
    mockGets(ACCESS_OPEN)
    const w = mountCard('oidc', 'client-abc')
    await flushPromises()
    expect(w.text()).toContain(en.admin.access.empty)
  })
})
