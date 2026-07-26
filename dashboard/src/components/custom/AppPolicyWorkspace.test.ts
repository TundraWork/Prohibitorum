import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import type {
  AccountSummary,
  AppAccessWorkspace,
  AppGroup,
  AppKind,
  Condition,
  ManagedApplication,
  ManualDecision,
  Rule,
} from '@/lib/appAccess'
import type { Page } from '@/lib/pagination'

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn() },
}))

import { api } from '@/lib/api'
import AppPolicyWorkspace from './AppPolicyWorkspace.vue'

const get = vi.mocked(api.get)
const post = vi.mocked(api.post)
const put = vi.mocked(api.put)

interface ProviderDescriptor {
  slug: string
}

type WorkspaceWire = AppAccessWorkspace & { providers: ProviderDescriptor[] }
type WorkspaceMode = 'manager' | 'admin'

interface GroupPreview {
  account: AccountSummary
  matched: boolean
}

interface ExplanationNode {
  path: string
  label: string
  result: boolean
  children?: ExplanationNode[]
}

interface GroupExplanation {
  account: AccountSummary
  explanation: ExplanationNode
}

const APP_KIND: AppKind = 'oidc'
const APP_ID = 'client/alpha'
const BASE = '/api/prohibitorum/managed-applications/oidc/client%2Falpha'
const ACCESS_ENDPOINT = `${BASE}/access`
const GROUPS_ENDPOINT = `${BASE}/groups`
const ACCOUNTS_ENDPOINT = `${BASE}/accounts`
const MANUAL_DECISIONS_ENDPOINT = `${GROUPS_ENDPOINT}/10/decisions`
const PREVIEW_ENDPOINT = `${GROUPS_ENDPOINT}/21/preview`
const PREVIEW_NEXT_ENDPOINT = `${PREVIEW_ENDPOINT}?cursor=preview%3Aafter%2Falice%2B1`
const EXPLAIN_ENDPOINT = `${GROUPS_ENDPOINT}/21/explain/7`

const OPEN_APP: ManagedApplication = {
  kind: APP_KIND,
  appId: APP_ID,
  displayName: 'Atlas',
  launchUrl: 'https://atlas.example.test/',
  redirectUris: ['https://atlas.example.test/callback'],
  accessRestricted: false,
}

const RESTRICTED_APP: ManagedApplication = {
  ...OPEN_APP,
  accessRestricted: true,
}

const PROVIDERS: ProviderDescriptor[] = [
  { slug: 'corporate' },
  { slug: 'partners' },
]

const CORPORATE_CONDITION: Condition = {
  fact: 'connection.provider',
  provider: 'corporate',
}

const CORPORATE_RULE: Rule = {
  version: 1,
  condition: CORPORATE_CONDITION,
}

const MANUAL_GROUP: AppGroup = {
  id: 10,
  kind: 'manual',
  slug: 'atlas-manual',
  displayName: 'Atlas manual decisions',
  description: 'Explicit access exceptions',
  exposedToDownstream: false,
}

const RULE_GROUP: AppGroup = {
  id: 21,
  kind: 'rule',
  slug: 'corporate-users',
  displayName: 'Corporate users',
  description: 'Verified corporate connections',
  exposedToDownstream: true,
  rule: CORPORATE_RULE,
}

const PARTNER_RULE_GROUP: AppGroup = {
  id: 22,
  kind: 'rule',
  slug: 'partner-engineers',
  displayName: 'Partner engineers',
  description: 'Verified partner connections',
  exposedToDownstream: true,
  rule: {
    version: 1,
    condition: {
      op: 'all',
      children: [{ fact: 'connection.provider', provider: 'partners' }],
    },
  },
}

const UPDATED_RULE_GROUP: AppGroup = {
  ...RULE_GROUP,
  displayName: 'Corporate staff',
  rule: {
    version: 1,
    condition: { fact: 'connection.provider', provider: 'partners' },
  },
}

const OPEN_EMPTY_WORKSPACE: WorkspaceWire = {
  app: OPEN_APP,
  accessRestricted: false,
  providers: PROVIDERS,
  ruleGroups: [],
}

const OPEN_RULE_WORKSPACE: WorkspaceWire = {
  app: OPEN_APP,
  accessRestricted: false,
  providers: PROVIDERS,
  ruleGroups: [RULE_GROUP],
}

const OPEN_POLICY_WORKSPACE: WorkspaceWire = {
  app: OPEN_APP,
  accessRestricted: false,
  providers: PROVIDERS,
  manualGroup: MANUAL_GROUP,
  ruleGroups: [RULE_GROUP],
}

const RESTRICTED_EMPTY_WORKSPACE: WorkspaceWire = {
  app: RESTRICTED_APP,
  accessRestricted: true,
  providers: PROVIDERS,
  ruleGroups: [],
}

const ALICE: AccountSummary = { id: 7, username: 'alice', displayName: 'Alice Ng' }
const BOB: AccountSummary = { id: 42, username: 'bob', displayName: 'Bob Ruiz' }

const ACCOUNTS_PAGE: Page<AccountSummary> = {
  items: [ALICE, BOB],
  nextCursor: '',
}

const ALICE_DECISION: ManualDecision = {
  account: ALICE,
  effect: 'allow',
  updatedAt: '2026-07-25T09:30:00Z',
}

const BOB_DECISION: ManualDecision = {
  account: BOB,
  effect: 'allow',
  updatedAt: '2026-07-25T10:15:00Z',
}

const DECISIONS_PAGE: Page<ManualDecision> = {
  items: [ALICE_DECISION],
  nextCursor: '',
}

const PREVIEW_PAGE_ONE: Page<GroupPreview> = {
  items: [{ account: ALICE, matched: true }],
  nextCursor: 'preview:after/alice+1',
}

const PREVIEW_PAGE_TWO: Page<GroupPreview> = {
  items: [{ account: BOB, matched: false }],
  nextCursor: '',
}

const SAFE_EXPLANATION: GroupExplanation = {
  account: ALICE,
  explanation: {
    path: '$',
    label: 'all',
    result: true,
    children: [
      {
        path: '$.children[0]',
        label: 'connection.provider=corporate',
        result: true,
      },
      {
        path: '$.children[1]',
        label: 'not',
        result: true,
        children: [
          {
            path: '$.children[1].child',
            label: 'avatar=user_uploaded',
            result: false,
          },
        ],
      },
    ],
  },
}

type GetFixture = unknown | (() => unknown | Promise<unknown>)

function i18n() {
  return createI18n({
    legacy: false,
    locale: 'en',
    fallbackLocale: 'en',
    messages: { en },
  })
}


function mockWorkspaceGets(
  workspace: WorkspaceWire | (() => WorkspaceWire),
  routes: Record<string, GetFixture> = {},
): void {
  get.mockImplementation(async (path: string) => {
    if (path === ACCESS_ENDPOINT) {
      return typeof workspace === 'function' ? workspace() : workspace
    }
    if (path in routes) {
      const fixture = routes[path]!
      return typeof fixture === 'function' ? fixture() : fixture
    }

    const pathname = path.split('?')[0]
    if (pathname === ACCOUNTS_ENDPOINT) return ACCOUNTS_PAGE
    if (pathname === MANUAL_DECISIONS_ENDPOINT) return DECISIONS_PAGE
    if (pathname === `${GROUPS_ENDPOINT}/${RULE_GROUP.id}`) return RULE_GROUP

    throw new Error(`Unexpected GET ${path}`)
  })
}

function mountWorkspace(mode: WorkspaceMode = 'manager') {
  return mount(AppPolicyWorkspace, {
    props: {
      kind: APP_KIND,
      appId: APP_ID,
      displayName: OPEN_APP.displayName,
      mode,
    },
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

function clickDestructiveConfirm(label: string): void {
  const buttons = Array.from(document.body.querySelectorAll('button')).filter(
    (button) => button.getAttribute('data-variant') === 'destructive'
      && button.textContent?.includes(label),
  )
  const confirm = buttons[buttons.length - 1]
  expect(confirm).toBeDefined()
  confirm!.click()
}

async function chooseCondition(
  wrapper: VueWrapper,
  control: 'kind' | 'value',
  path: string,
  value: string,
): Promise<void> {
  const trigger = wrapper.get(`[data-test="condition-${control}-${path}"]`)
  if (trigger.element instanceof HTMLSelectElement) {
    await trigger.setValue(value)
    return
  }
  await trigger.trigger('keydown', { key: 'Enter' })
  await flushPromises()
  const option = document.body.querySelector<HTMLElement>(
    `[data-test="condition-option-${path}-${value}"]`,
  )
  expect(option).not.toBeNull()
  option!.dispatchEvent(new MouseEvent('pointerup', { bubbles: true, button: 0 }))
  await flushPromises()
}

async function conditionOptionValues(wrapper: VueWrapper, path: string): Promise<string[]> {
  const trigger = wrapper.get(`[data-test="condition-value-${path}"]`)
  if (trigger.element instanceof HTMLSelectElement) {
    return Array.from(trigger.element.options).map((option) => option.value).filter(Boolean)
  }
  await trigger.trigger('keydown', { key: 'Enter' })
  await flushPromises()
  return Array.from(
    document.body.querySelectorAll<HTMLElement>(`[data-test^="condition-option-${path}-"]`),
  ).map((option) => option.dataset.test!.replace(`condition-option-${path}-`, ''))
}

beforeEach(() => {
  get.mockReset()
  post.mockReset()
  put.mockReset()
  document.body.innerHTML = ''
})

afterEach(() => {
  document.body.innerHTML = ''
})

describe('AppPolicyWorkspace', () => {
  it('shows loading, uses the encoded app endpoint, and projects provider descriptors into rule editing', async () => {
    const access = deferred<WorkspaceWire>()
    get.mockImplementation((path: string) => {
      if (path === ACCESS_ENDPOINT) return access.promise
      if (path.split('?')[0] === ACCOUNTS_ENDPOINT) return Promise.resolve(ACCOUNTS_PAGE)
      if (path.split('?')[0] === MANUAL_DECISIONS_ENDPOINT) return Promise.resolve(DECISIONS_PAGE)
      throw new Error(`Unexpected GET ${path}`)
    })

    const wrapper = mountWorkspace()
    await Promise.resolve()

    expect(wrapper.get('[data-test="policy-loading"]').text()).toMatch(/loading/i)
    expect(get).toHaveBeenCalledWith(ACCESS_ENDPOINT)

    access.resolve(OPEN_POLICY_WORKSPACE)
    await flushPromises()

    expect(wrapper.find('[data-test="policy-loading"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Atlas')
    expect(wrapper.text()).toContain('Atlas manual decisions')
    expect(wrapper.text()).toContain('Corporate users')

    await wrapper.get('[data-test="rule-group-edit-21"]').trigger('click')
    const form = wrapper.get('[data-test="rule-group-form-21"]')
    expect(form.get('[data-test="condition-kind-root"]').text()).toContain('Connection provider')
    expect(form.get('[data-test="condition-value-root"]').text()).toContain('corporate')
    expect(await conditionOptionValues(form, 'root')).toEqual(['corporate', 'partners'])
  })

  it.each([
    {
      restricted: false,
      label: en.manage.applications.open,
      hint: en.manage.applications.openDescription,
    },
    {
      restricted: true,
      label: en.manage.applications.restricted,
      hint: en.manage.applications.restrictedDescription,
    },
  ])('shows the $label state and matching access hint', async ({ restricted, label, hint }) => {
    const workspace: WorkspaceWire = {
      app: restricted ? RESTRICTED_APP : OPEN_APP,
      accessRestricted: restricted,
      providers: PROVIDERS,
      ruleGroups: [],
    }
    mockWorkspaceGets(workspace)

    const wrapper = mountWorkspace()
    await flushPromises()

    expect(wrapper.get('[data-test="restriction-state"]').text()).toContain(label)
    expect(wrapper.get('[data-test="restriction-hint"]').text()).toContain(hint)
  })

  it('creates one manual group and removes the create control after success', async () => {
    let created = false
    mockWorkspaceGets(() => created
      ? { ...OPEN_EMPTY_WORKSPACE, manualGroup: MANUAL_GROUP }
      : OPEN_EMPTY_WORKSPACE)
    post.mockImplementation(async () => {
      created = true
      return MANUAL_GROUP
    })

    const wrapper = mountWorkspace()
    await flushPromises()
    await wrapper.get('[data-test="manual-group-create"]').trigger('click')

    const form = wrapper.get('[data-test="manual-group-form"]')
    await form.get('input[name="slug"]').setValue('atlas-manual')
    await form.get('input[name="displayName"]').setValue('Atlas manual decisions')
    await form.get('textarea[name="description"]').setValue('Explicit access exceptions')
    await form.get('[data-test="manual-group-save"]').trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(GROUPS_ENDPOINT, {
      kind: 'manual',
      slug: 'atlas-manual',
      displayName: 'Atlas manual decisions',
      description: 'Explicit access exceptions',
      exposedToDownstream: false,
    })
    expect(wrapper.text()).toContain('Atlas manual decisions')
    expect(wrapper.find('[data-test="manual-group-create"]').exists()).toBe(false)
  })

  it('creates a rule group with the closed rule document payload', async () => {
    let created = false
    mockWorkspaceGets(() => created
      ? { ...OPEN_EMPTY_WORKSPACE, ruleGroups: [PARTNER_RULE_GROUP] }
      : OPEN_EMPTY_WORKSPACE)
    post.mockImplementation(async () => {
      created = true
      return PARTNER_RULE_GROUP
    })

    const wrapper = mountWorkspace()
    await flushPromises()
    await wrapper.get('[data-test="rule-group-create"]').trigger('click')

    const form = wrapper.get('[data-test="rule-group-form-new"]')
    await form.get('input[name="slug"]').setValue('partner-engineers')
    await form.get('input[name="displayName"]').setValue('Partner engineers')
    await form.get('textarea[name="description"]').setValue('Verified partner connections')
    await chooseCondition(form, 'kind', 'root-0', 'connection.provider')
    await chooseCondition(form, 'value', 'root-0', 'partners')
    await form.get('[data-test="rule-group-save-new"]').trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(GROUPS_ENDPOINT, {
      kind: 'rule',
      slug: 'partner-engineers',
      displayName: 'Partner engineers',
      description: 'Verified partner connections',
      exposedToDownstream: true,
      rule: {
        version: 1,
        condition: {
          op: 'all',
          children: [{ fact: 'connection.provider', provider: 'partners' }],
        },
      },
    })
    expect(wrapper.text()).toContain('Partner engineers')
  })

  it('blocks rule submission while a newly added combinator is empty', async () => {
    mockWorkspaceGets(OPEN_EMPTY_WORKSPACE)
    const wrapper = mountWorkspace()
    await flushPromises()
    await wrapper.get('[data-test="rule-group-create"]').trigger('click')

    const form = wrapper.get('[data-test="rule-group-form-new"]')
    await form.get('input[name="slug"]').setValue('nested-policy')
    await form.get('input[name="displayName"]').setValue('Nested policy')
    await form.get('[data-test="add-all-root"]').trigger('click')

    const save = form.get<HTMLButtonElement>('[data-test="rule-group-save-new"]')
    expect(save.element.disabled).toBe(true)
    await save.trigger('click')
    expect(post).not.toHaveBeenCalled()
  })

  it('updates a rule group without sending its immutable kind or app binding', async () => {
    let updated = false
    mockWorkspaceGets(() => updated
      ? { ...OPEN_RULE_WORKSPACE, ruleGroups: [UPDATED_RULE_GROUP] }
      : OPEN_RULE_WORKSPACE)
    put.mockImplementation(async () => {
      updated = true
      return UPDATED_RULE_GROUP
    })

    const wrapper = mountWorkspace()
    await flushPromises()
    await wrapper.get('[data-test="rule-group-edit-21"]').trigger('click')

    const form = wrapper.get('[data-test="rule-group-form-21"]')
    await form.get('input[name="displayName"]').setValue('Corporate staff')
    await chooseCondition(form, 'value', 'root', 'partners')
    await form.get('[data-test="rule-group-save-21"]').trigger('click')
    await flushPromises()

    expect(put).toHaveBeenCalledWith(`${GROUPS_ENDPOINT}/21`, {
      slug: 'corporate-users',
      displayName: 'Corporate staff',
      description: 'Verified corporate connections',
      exposedToDownstream: true,
      rule: {
        version: 1,
        condition: { fact: 'connection.provider', provider: 'partners' },
      },
    })
    expect(wrapper.text()).toContain('Corporate staff')
  })

  it('deletes a rule group only after ConfirmDialog confirmation', async () => {
    let deleted = false
    mockWorkspaceGets(() => deleted ? OPEN_EMPTY_WORKSPACE : OPEN_RULE_WORKSPACE)
    post.mockImplementation(async () => {
      deleted = true
      return {}
    })

    const wrapper = mountWorkspace()
    await flushPromises()
    await wrapper.get('[data-test="rule-group-delete-21"]').trigger('click')
    await flushPromises()

    expect(document.body.textContent).toContain('Corporate users')
    expect(post).not.toHaveBeenCalled()

    clickDestructiveConfirm('Delete')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(`${GROUPS_ENDPOINT}/21/delete`)
    expect(wrapper.find('[data-test="rule-group-row-21"]').exists()).toBe(false)
  })

  it('paginates calculated rule preview with the server nextCursor', async () => {
    mockWorkspaceGets(OPEN_RULE_WORKSPACE, {
      [PREVIEW_ENDPOINT]: PREVIEW_PAGE_ONE,
      [PREVIEW_NEXT_ENDPOINT]: PREVIEW_PAGE_TWO,
    })

    const wrapper = mountWorkspace()
    await flushPromises()
    await wrapper.get('[data-test="rule-group-preview-21"]').trigger('click')
    await flushPromises()

    expect(get).toHaveBeenCalledWith(PREVIEW_ENDPOINT)
    expect(wrapper.get('[data-test="preview-row-7"]').text()).toContain('Alice Ng')

    const panel = wrapper.get('[data-test="preview-panel-21"]')
    await panel.get('[data-test="next-page"]').trigger('click')
    await flushPromises()

    expect(get).toHaveBeenCalledWith(PREVIEW_NEXT_ENDPOINT)
    expect(wrapper.find('[data-test="preview-row-7"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="preview-row-42"]').text()).toContain('Bob Ruiz')
  })

  it('renders a nested explanation from only account plus label, path, and result fields', async () => {
    mockWorkspaceGets(OPEN_RULE_WORKSPACE, {
      [PREVIEW_ENDPOINT]: PREVIEW_PAGE_ONE,
      [EXPLAIN_ENDPOINT]: SAFE_EXPLANATION,
    })

    const wrapper = mountWorkspace()
    await flushPromises()
    await wrapper.get('[data-test="rule-group-preview-21"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="preview-explain-21-7"]').trigger('click')
    await flushPromises()

    expect(get).toHaveBeenCalledWith(EXPLAIN_ENDPOINT)
    expect(wrapper.get('[data-test="explanation-panel-21-7"]').text()).toContain('Alice Ng')

    const root = wrapper.get('[data-test="explanation-node-root"]')
    const provider = wrapper.get('[data-test="explanation-node-root-0"]')
    const negate = wrapper.get('[data-test="explanation-node-root-1"]')
    const avatar = wrapper.get('[data-test="explanation-node-root-1-0"]')

    expect(root.text()).toContain('$')
    expect(root.text()).toContain('all')
    expect(root.text()).toContain('Matches')
    expect(provider.text()).toContain('$.children[0]')
    expect(provider.text()).toContain('connection.provider=corporate')
    expect(provider.text()).toContain('Matches')
    expect(negate.text()).toContain('$.children[1]')
    expect(negate.text()).toContain('not')
    expect(negate.text()).toContain('Matches')
    expect(avatar.text()).toContain('$.children[1].child')
    expect(avatar.text()).toContain('avatar=user_uploaded')
    expect(avatar.text()).toContain('Does not match')
  })

  it('forwards manual allow and clear actions to the manual-group policy endpoints', async () => {
    mockWorkspaceGets(OPEN_POLICY_WORKSPACE)
    post.mockImplementation(async (path: string) => {
      if (path === MANUAL_DECISIONS_ENDPOINT) return BOB_DECISION
      if (path === `${MANUAL_DECISIONS_ENDPOINT}/clear`) return {}
      throw new Error(`Unexpected POST ${path}`)
    })

    const wrapper = mountWorkspace()
    await flushPromises()

    expect(get).toHaveBeenCalledWith(expect.stringContaining(ACCOUNTS_ENDPOINT))
    expect(get).toHaveBeenCalledWith(expect.stringContaining(MANUAL_DECISIONS_ENDPOINT))

    const search = wrapper.get('[data-test="manual-account-search"]')
    await search.setValue('bob')
    await flushPromises()
    expect(wrapper.find('[data-test="manual-account-result-42"]').exists()).toBe(true)
    await wrapper.get('[data-test="manual-set-allow-42"]').trigger('click')
    await flushPromises()

    expect(post).toHaveBeenNthCalledWith(1, MANUAL_DECISIONS_ENDPOINT, {
      accountId: 42,
      effect: 'allow',
    })

    await search.setValue('')
    await flushPromises()
    await wrapper.get('[data-test="manual-clear-7"]').trigger('click')
    await flushPromises()

    expect(post).toHaveBeenNthCalledWith(2, `${MANUAL_DECISIONS_ENDPOINT}/clear`, {
      accountId: 7,
    })
  })

  it('confirms before enabling restriction when no policy group exists', async () => {
    let restricted = false
    mockWorkspaceGets(() => restricted ? RESTRICTED_EMPTY_WORKSPACE : OPEN_EMPTY_WORKSPACE)
    post.mockImplementation(async () => {
      restricted = true
      return RESTRICTED_APP
    })

    const wrapper = mountWorkspace()
    await flushPromises()
    await wrapper.get('[data-test="access-restricted-toggle"]').trigger('click')
    await flushPromises()

    expect(post).not.toHaveBeenCalled()
    expect(document.body.textContent).toMatch(/no policy groups/i)

    clickDestructiveConfirm('Enable')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(`${ACCESS_ENDPOINT}/set-restricted`, { restricted: true })
    expect(wrapper.get('[data-test="restriction-hint"]').text())
      .toContain(en.manage.applications.restrictedDescription)
  })

  it('enables restriction directly when at least one policy group exists', async () => {
    let restricted = false
    mockWorkspaceGets(() => restricted
      ? { ...OPEN_RULE_WORKSPACE, app: RESTRICTED_APP, accessRestricted: true }
      : OPEN_RULE_WORKSPACE)
    post.mockImplementation(async () => {
      restricted = true
      return RESTRICTED_APP
    })

    const wrapper = mountWorkspace()
    await flushPromises()
    await wrapper.get('[data-test="access-restricted-toggle"]').trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(`${ACCESS_ENDPOINT}/set-restricted`, { restricted: true })
    const enableConfirms = Array.from(document.body.querySelectorAll('button')).filter(
      (button) => button.getAttribute('data-variant') === 'destructive'
        && button.textContent?.includes('Enable'),
    )
    expect(enableConfirms).toHaveLength(0)
    expect(wrapper.get('[data-test="restriction-hint"]').text())
      .toContain(en.manage.applications.restrictedDescription)
  })

  it.each(['manager', 'admin'] as const)(
    '%s mode remains policy-only and exposes no protocol configuration controls',
    async (mode) => {
      mockWorkspaceGets(OPEN_EMPTY_WORKSPACE)

      const wrapper = mountWorkspace(mode)
      await flushPromises()

      expect(get.mock.calls.map(([path]) => path)).toEqual([ACCESS_ENDPOINT])
      expect(wrapper.get('[data-test="app-policy-workspace"]').text()).toContain('Atlas')
      expect(wrapper.find('[data-test="protocol-configuration"]').exists()).toBe(false)
      expect(wrapper.find('input[name="redirectUris"]').exists()).toBe(false)
      expect(wrapper.find('input[name="clientSecret"]').exists()).toBe(false)
      expect(wrapper.find('input[name="entityId"]').exists()).toBe(false)
      expect(wrapper.text()).not.toContain('https://atlas.example.test/callback')
      expect(post).not.toHaveBeenCalled()
      expect(put).not.toHaveBeenCalled()
    },
  )
})
