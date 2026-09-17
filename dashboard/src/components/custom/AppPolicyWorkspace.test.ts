import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { RouterView, createMemoryHistory, createRouter } from 'vue-router'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import type {
  AccountSummary,
  AppAccessWorkspace,
  AppGroup,
  AppKind,
  ManagedApplication,
  ManualDecision,
  ProviderDescriptor,
  Rule,
} from '@/lib/appAccess'
import type { Page } from '@/lib/pagination'

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn() },
}))

import { api } from '@/lib/api'
import AppPolicyWorkspace from './AppPolicyWorkspace.vue'
import RuleEditor, { type RuleEditorDraft } from './RuleEditor.vue'

const get = vi.mocked(api.get)
const post = vi.mocked(api.post)
const put = vi.mocked(api.put)

type WorkspaceWire = AppAccessWorkspace
type WorkspaceMode = 'manager' | 'admin'

const mounted: VueWrapper[] = []

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
  { slug: 'corporate', displayName: 'Corporate identity' },
  { slug: 'partners', displayName: 'Partner directory' },
]

const CORPORATE_RULE: Rule = {
  version: 1,
  condition: { fact: 'connection.provider', provider: 'corporate' },
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

const CREATE_RULE_DRAFT: RuleEditorDraft = {
  slug: 'partner-engineers',
  displayName: 'Partner engineers',
  description: 'Verified partner connections',
  exposedToDownstream: true,
  rule: PARTNER_RULE_GROUP.rule!,
}

const UPDATE_RULE_DRAFT: RuleEditorDraft = {
  slug: 'corporate-users',
  displayName: 'Corporate staff',
  description: 'Verified corporate connections',
  exposedToDownstream: true,
  rule: UPDATED_RULE_GROUP.rule!,
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
  const wrapper = mount(AppPolicyWorkspace, {
    props: {
      kind: APP_KIND,
      appId: APP_ID,
      displayName: OPEN_APP.displayName,
      mode,
    },
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })
  mounted.push(wrapper)
  return wrapper
}

async function mountRoutedWorkspace(path = '/manage/applications/oidc/client%2Falpha') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/manage/applications/:kind/:id',
        component: AppPolicyWorkspace,
        props: (route) => ({
          kind: route.params.kind as AppKind,
          appId: route.params.id as string,
          displayName: route.params.id === APP_ID ? OPEN_APP.displayName : 'Knowledge base',
          mode: 'manager' as const,
        }),
      },
      { path: '/elsewhere', component: { template: '<p>Elsewhere</p>' } },
    ],
  })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(RouterView, {
    global: { plugins: [router, i18n()] },
    attachTo: document.body,
  })
  mounted.push(wrapper)
  await flushPromises()
  return { router, wrapper }
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

function ruleEditor(wrapper: ReturnType<typeof mount>) {
  return wrapper.getComponent(RuleEditor)
}
async function chooseRuleAction(wrapper: VueWrapper, groupId: number, action: 'edit' | 'delete'): Promise<void> {
  await wrapper.get(`[data-test="rule-group-actions-${groupId}"]`).trigger('click')
  await flushPromises()
  document.body.querySelector<HTMLElement>(`[data-test="rule-group-${action}-${groupId}"]`)!
    .dispatchEvent(new Event('click', { bubbles: true }))
  await flushPromises()
}

function clickConfirmCancel(): void {
  const buttons = Array.from(document.body.querySelectorAll('button')).filter(
    (button) => button.textContent?.trim() === en.confirm.cancel,
  )
  const cancel = buttons.at(-1)
  expect(cancel).toBeDefined()
  cancel!.click()
}

beforeEach(() => {
  get.mockReset()
  post.mockReset()
  put.mockReset()
  document.body.innerHTML = ''
})

afterEach(() => {
  while (mounted.length) mounted.pop()!.unmount()
  document.body.innerHTML = ''
})

describe('AppPolicyWorkspace', () => {
  it('creates an in-list draft row and expands edit inside the selected rule row', async () => {
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
    access.resolve(OPEN_POLICY_WORKSPACE)
    await flushPromises()

    await wrapper.get('[data-test="rule-group-create"]').trigger('click')
    const draftRow = wrapper.get('[data-test="rule-group-draft-row"]')
    expect(draftRow.get('[data-test="rule-editor"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="rule-groups-list"]').element.firstElementChild).toBe(draftRow.element)
    let editor = ruleEditor(wrapper)
    expect(editor.props('mode')).toBe('create')
    expect(editor.props('providers')).toEqual(PROVIDERS)
    expect(editor.props('previewEndpoint')).toBe(`${BASE}/rule-preview`)
    expect(editor.props('initialDraft')).toEqual({
      slug: '',
      displayName: '',
      description: '',
      exposedToDownstream: true,
      rule: { version: 1, condition: { op: 'all', children: [{}] } },
    })

    editor.vm.$emit('cancel')
    await flushPromises()
    await chooseRuleAction(wrapper, 21, 'edit')

    const selectedRow = wrapper.get('[data-test="rule-group-row-21"]')
    expect(selectedRow.get('[data-test="rule-editor"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="rule-group-draft-row"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-test="rule-editor"]')).toHaveLength(1)
    editor = ruleEditor(wrapper)
    expect(editor.props('mode')).toBe('edit')
    expect(editor.props('initialDraft')).toEqual({
      slug: RULE_GROUP.slug,
      displayName: RULE_GROUP.displayName,
      description: RULE_GROUP.description,
      exposedToDownstream: true,
      rule: CORPORATE_RULE,
    })
  })

  it('mounts outside a router without route-injection warnings', async () => {
    mockWorkspaceGets(OPEN_EMPTY_WORKSPACE)
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})

    try {
      mountWorkspace()
      await flushPromises()
      expect(warn.mock.calls.flat().join(' ')).not.toMatch(/router|active route record/i)
    } finally {
      warn.mockRestore()
    }
  })

  it('confirms a dirty draft before applying a direct application identity change', async () => {
    const secondBase = '/api/prohibitorum/managed-applications/saml/44'
    const secondAccessEndpoint = `${secondBase}/access`
    const secondWorkspace: WorkspaceWire = {
      app: {
        kind: 'saml',
        appId: '44',
        displayName: 'Knowledge base',
        accessRestricted: false,
      },
      accessRestricted: false,
      providers: [],
      ruleGroups: [],
    }
    get.mockImplementation(async (path: string) => {
      if (path === ACCESS_ENDPOINT) return OPEN_RULE_WORKSPACE
      if (path === secondAccessEndpoint) return secondWorkspace
      throw new Error(`Unexpected GET ${path}`)
    })

    const wrapper = mountWorkspace()
    await flushPromises()
    await chooseRuleAction(wrapper, 21, 'edit')
    ruleEditor(wrapper).vm.$emit('dirty-change', true)
    await flushPromises()

    await wrapper.setProps({ kind: 'saml', appId: '44', displayName: 'Knowledge base' })
    await flushPromises()

    expect(document.body.textContent).toContain('Discard unsaved changes?')
    expect(wrapper.text()).toContain('Atlas')
    expect(get).not.toHaveBeenCalledWith(secondAccessEndpoint)

    clickDestructiveConfirm('Discard changes')
    await flushPromises()
    expect(get).toHaveBeenCalledWith(secondAccessEndpoint, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.text()).toContain('Knowledge base')
  })

  it('keeps editing on an aborted route leave and navigates after confirmed discard', async () => {
    mockWorkspaceGets(OPEN_RULE_WORKSPACE)
    const { router, wrapper } = await mountRoutedWorkspace()
    const workspaceWrapper = wrapper.getComponent(AppPolicyWorkspace)
    await chooseRuleAction(workspaceWrapper, 21, 'edit')
    ruleEditor(workspaceWrapper).vm.$emit('dirty-change', true)
    await flushPromises()

    await router.push('/elsewhere')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).not.toBe('/elsewhere')
    expect(document.body.textContent).toContain('Discard unsaved changes?')

    clickConfirmCancel()
    await flushPromises()
    expect(router.currentRoute.value.fullPath).not.toBe('/elsewhere')
    expect(workspaceWrapper.find('[data-test="rule-editor"]').exists()).toBe(true)

    await router.push('/elsewhere')
    await flushPromises()
    clickDestructiveConfirm('Discard changes')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/elsewhere')
  })

  it('keeps editing on an aborted route update and loads the new application after confirmed discard', async () => {
    const secondAccessEndpoint = '/api/prohibitorum/managed-applications/saml/44/access'
    const secondWorkspace: WorkspaceWire = {
      app: {
        kind: 'saml',
        appId: '44',
        displayName: 'Knowledge base',
        accessRestricted: false,
      },
      accessRestricted: false,
      providers: [],
      ruleGroups: [],
    }
    get.mockImplementation(async (path: string) => {
      if (path === ACCESS_ENDPOINT) return OPEN_RULE_WORKSPACE
      if (path === secondAccessEndpoint) return secondWorkspace
      throw new Error(`Unexpected GET ${path}`)
    })
    const { router, wrapper } = await mountRoutedWorkspace()
    const workspaceWrapper = wrapper.getComponent(AppPolicyWorkspace)
    await chooseRuleAction(workspaceWrapper, 21, 'edit')
    ruleEditor(workspaceWrapper).vm.$emit('dirty-change', true)
    await flushPromises()

    await router.push('/manage/applications/saml/44')
    await flushPromises()
    expect(router.currentRoute.value.params.id).toBe(APP_ID)
    expect(document.body.textContent).toContain('Discard unsaved changes?')
    expect(get).not.toHaveBeenCalledWith(secondAccessEndpoint)

    clickDestructiveConfirm('Discard changes')
    await flushPromises()
    expect(router.currentRoute.value.params.id).toBe('44')
    expect(get).toHaveBeenCalledWith(secondAccessEndpoint, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.text()).toContain('Knowledge base')
  })

  it('reloads and resets policy state when the application identity changes', async () => {
    const secondBase = '/api/prohibitorum/managed-applications/saml/44'
    const secondAccessEndpoint = `${secondBase}/access`
    const secondApp: ManagedApplication = {
      kind: 'saml',
      appId: '44',
      displayName: 'Knowledge base',
      accessRestricted: false,
    }
    const secondWorkspace: WorkspaceWire = {
      app: secondApp,
      accessRestricted: false,
      providers: [],
      ruleGroups: [{ ...RULE_GROUP, id: 81, displayName: 'Knowledge base members' }],
    }
    get.mockImplementation(async (path: string) => {
      if (path === ACCESS_ENDPOINT) return OPEN_RULE_WORKSPACE
      if (path === secondAccessEndpoint) return secondWorkspace
      throw new Error(`Unexpected GET ${path}`)
    })
    post.mockResolvedValue({ ...secondApp, accessRestricted: true })

    const wrapper = mountWorkspace()
    await flushPromises()
    expect(wrapper.text()).toContain('Atlas')

    await wrapper.setProps({
      kind: 'saml',
      appId: '44',
      displayName: secondApp.displayName,
    })
    await flushPromises()

    expect(get).toHaveBeenCalledWith(secondAccessEndpoint, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.text()).toContain('Knowledge base')
    expect(wrapper.text()).not.toContain('Atlas')

    await wrapper.get('[data-test="access-restricted-toggle"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith(`${secondAccessEndpoint}/set-restricted`, {
      restricted: true,
    })
  })

  it('discards an in-flight workspace response after the application identity changes', async () => {
    const firstAccess = deferred<WorkspaceWire>()
    const secondAccessEndpoint =
      '/api/prohibitorum/managed-applications/forward_auth/proxy/access'
    const secondWorkspace: WorkspaceWire = {
      app: {
        kind: 'forward_auth',
        appId: 'proxy',
        displayName: 'Protected proxy',
        accessRestricted: false,
      },
      accessRestricted: false,
      providers: [],
      ruleGroups: [],
    }
    get.mockImplementation((path: string) => {
      if (path === ACCESS_ENDPOINT) return firstAccess.promise
      if (path === secondAccessEndpoint) return Promise.resolve(secondWorkspace)
      throw new Error(`Unexpected GET ${path}`)
    })

    const wrapper = mountWorkspace()
    await Promise.resolve()
    await wrapper.setProps({
      kind: 'forward_auth',
      appId: 'proxy',
      displayName: 'Protected proxy',
    })
    firstAccess.resolve(OPEN_RULE_WORKSPACE)
    await flushPromises()

    expect(get).toHaveBeenCalledWith(secondAccessEndpoint, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.text()).toContain('Protected proxy')
    expect(wrapper.text()).not.toContain('Atlas')
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

  it('creates a rule group from the shared editor save event with the exact immutable binding payload', async () => {
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
    ruleEditor(wrapper).vm.$emit('save', CREATE_RULE_DRAFT)
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

  it('keeps the shared editor busy through workspace reload so a completed save cannot be submitted twice', async () => {
    const reloaded = deferred<WorkspaceWire>()
    let accessRequests = 0
    get.mockImplementation((path: string) => {
      if (path === ACCESS_ENDPOINT) {
        accessRequests += 1
        return accessRequests === 1 ? Promise.resolve(OPEN_EMPTY_WORKSPACE) : reloaded.promise
      }
      if (path.split('?')[0] === ACCOUNTS_ENDPOINT) return Promise.resolve(ACCOUNTS_PAGE)
      if (path.split('?')[0] === MANUAL_DECISIONS_ENDPOINT) return Promise.resolve(DECISIONS_PAGE)
      throw new Error(`Unexpected GET ${path}`)
    })
    post.mockResolvedValue(PARTNER_RULE_GROUP)

    const wrapper = mountWorkspace()
    await flushPromises()
    await wrapper.get('[data-test="rule-group-create"]').trigger('click')
    ruleEditor(wrapper).vm.$emit('save', CREATE_RULE_DRAFT)
    await flushPromises()

    expect(ruleEditor(wrapper).props('busy')).toBe(true)
    ruleEditor(wrapper).vm.$emit('save', CREATE_RULE_DRAFT)
    await flushPromises()
    expect(post).toHaveBeenCalledTimes(1)

    reloaded.resolve({ ...OPEN_EMPTY_WORKSPACE, ruleGroups: [PARTNER_RULE_GROUP] })
    await flushPromises()
    expect(wrapper.find('[data-test="rule-editor"]').exists()).toBe(false)
  })
  it('updates a rule group from the shared editor save event without sending immutable kind or app binding', async () => {
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
    await chooseRuleAction(wrapper, 21, 'edit')
    ruleEditor(wrapper).vm.$emit('save', UPDATE_RULE_DRAFT)
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
    expect(wrapper.get('[data-test="rule-save-announcement"]').text()).toContain('Rule group saved.')
    expect(document.activeElement).toBe(wrapper.get('[data-test="rule-group-row-21"]').element)
  })

  it('requires a discard decision before opening create, another edit, or a saved preview from a dirty editor', async () => {
    mockWorkspaceGets({ ...OPEN_RULE_WORKSPACE, ruleGroups: [RULE_GROUP, PARTNER_RULE_GROUP] }, {
      [PREVIEW_ENDPOINT]: PREVIEW_PAGE_ONE,
    })
    const wrapper = mountWorkspace()
    await flushPromises()
    await chooseRuleAction(wrapper, 21, 'edit')
    ruleEditor(wrapper).vm.$emit('dirty-change', true)
    await flushPromises()

    await wrapper.get('[data-test="rule-group-create"]').trigger('click')
    expect(document.body.textContent).toContain('Discard unsaved changes?')
    expect(ruleEditor(wrapper).props('initialDraft')).toMatchObject({ slug: RULE_GROUP.slug })
    clickConfirmCancel()
    await flushPromises()

    await chooseRuleAction(wrapper, 22, 'edit')
    expect(document.body.textContent).toContain('Discard unsaved changes?')
    expect(ruleEditor(wrapper).props('initialDraft')).toMatchObject({ slug: RULE_GROUP.slug })
    clickConfirmCancel()
    await flushPromises()

    await wrapper.get('[data-test="rule-group-preview-22"]').trigger('click')
    expect(document.body.textContent).toContain('Discard unsaved changes?')
    expect(wrapper.find('[data-test="preview-panel-22"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="rule-editor"]').exists()).toBe(true)

    clickDestructiveConfirm('Discard changes')
    await flushPromises()
    expect(wrapper.find('[data-test="rule-editor"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="preview-panel-22"]').exists()).toBe(true)
  })

  it('remounts the same editor target after confirmed discard so its local draft is reset', async () => {
    mockWorkspaceGets(OPEN_EMPTY_WORKSPACE)
    const wrapper = mountWorkspace()
    await flushPromises()
    await wrapper.get('[data-test="rule-group-create"]').trigger('click')
    await ruleEditor(wrapper).get('[data-test="rule-display-name"]').setValue('Abandoned rule')
    await flushPromises()

    await wrapper.get('[data-test="rule-group-create"]').trigger('click')
    expect(document.body.textContent).toContain('Discard unsaved changes?')
    clickDestructiveConfirm('Discard changes')
    await flushPromises()

    expect((ruleEditor(wrapper).get('[data-test="rule-display-name"]').element as HTMLInputElement).value)
      .toBe('')
  })

  it('opens each saved-rule ellipsis menu and routes edit or delete actions', async () => {
    mockWorkspaceGets({ ...OPEN_RULE_WORKSPACE, ruleGroups: [RULE_GROUP, PARTNER_RULE_GROUP] })
    const wrapper = mountWorkspace()
    await flushPromises()

    for (const group of [RULE_GROUP, PARTNER_RULE_GROUP]) {
      const trigger = wrapper.get(`[data-test="rule-group-actions-${group.id}"]`)
      expect(trigger.attributes('data-slot')).toBe('dropdown-menu-trigger')
      await trigger.trigger('click')
      await flushPromises()
      const edit = document.body.querySelector<HTMLElement>(`[data-test="rule-group-edit-${group.id}"]`)
      const remove = document.body.querySelector<HTMLElement>(`[data-test="rule-group-delete-${group.id}"]`)
      expect(edit?.textContent).toContain('Edit')
      expect(remove?.textContent).toContain('Delete')
      edit!.dispatchEvent(new Event('click', { bubbles: true }))
      await flushPromises()
      expect(wrapper.get(`[data-test="rule-group-row-${group.id}"]`).get('[data-test="rule-editor"]').exists()).toBe(true)
      ruleEditor(wrapper).vm.$emit('cancel')
      await flushPromises()
    }
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
    await wrapper.get('[data-test="rule-group-actions-21"]').trigger('click')
    await flushPromises()
    document.body.querySelector<HTMLElement>('[data-test="rule-group-delete-21"]')!.click()
    await flushPromises()

    expect(document.body.textContent).toContain('Corporate users')
    expect(post).not.toHaveBeenCalled()

    clickDestructiveConfirm('Delete')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(`${GROUPS_ENDPOINT}/21/delete`)
    expect(wrapper.find('[data-test="rule-group-row-21"]').exists()).toBe(false)
  })

  it('renders compact saved-rule meaning with a disclosure for the full read-only outline', async () => {
    mockWorkspaceGets(OPEN_RULE_WORKSPACE)
    const wrapper = mountWorkspace()
    await flushPromises()

    const row = wrapper.get('[data-test="rule-group-row-21"]')
    expect(row.get('[data-test="rule-meaning-counts"]').text()).toContain('1 condition')
    expect(row.get('[data-test="rule-meaning-first-line"]').text()).toContain('Corporate identity')
    const fullMeaning = row.get('[data-test="rule-meaning-disclosure-21"]')
    expect(fullMeaning.text()).toContain('Plain meaning')
    expect(fullMeaning.get('[data-test="rule-meaning"]').text()).toContain('Corporate identity')
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

    expect(get).toHaveBeenCalledWith(PREVIEW_ENDPOINT, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.get('[data-test="preview-row-7"]').text()).toContain('Alice Ng')

    const panel = wrapper.get('[data-test="preview-panel-21"]')
    await panel.get('[data-test="next-page"]').trigger('click')
    await flushPromises()

    expect(get).toHaveBeenCalledWith(PREVIEW_NEXT_ENDPOINT, expect.objectContaining({ signal: expect.any(AbortSignal) }))
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

    expect(get).toHaveBeenCalledWith(EXPLAIN_ENDPOINT, expect.objectContaining({ signal: expect.any(AbortSignal) }))
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

    expect(get).toHaveBeenCalledWith(expect.stringContaining(ACCOUNTS_ENDPOINT), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(get).toHaveBeenCalledWith(expect.stringContaining(MANUAL_DECISIONS_ENDPOINT), expect.objectContaining({ signal: expect.any(AbortSignal) }))

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

  it('excludes accounts with decisions on later cursor pages from neutral search', async () => {
    const nextDecisionsEndpoint = `${MANUAL_DECISIONS_ENDPOINT}?cursor=next`
    const offPageDeny: ManualDecision = {
      account: BOB,
      effect: 'deny',
      updatedAt: '2026-07-25T11:00:00Z',
    }
    get.mockImplementation(async (path: string) => {
      if (path === ACCESS_ENDPOINT) return OPEN_POLICY_WORKSPACE
      if (path.split('?')[0] === ACCOUNTS_ENDPOINT) return ACCOUNTS_PAGE
      if (path === MANUAL_DECISIONS_ENDPOINT) {
        return { items: [ALICE_DECISION], nextCursor: 'next' }
      }
      if (path === nextDecisionsEndpoint) {
        return { items: [offPageDeny], nextCursor: '' }
      }
      throw new Error(`Unexpected GET ${path}`)
    })

    const wrapper = mountWorkspace()
    await flushPromises()

    expect(get).toHaveBeenCalledWith(nextDecisionsEndpoint, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    await wrapper.get('[data-test="manual-account-search"]').setValue('bob')
    await flushPromises()

    expect(wrapper.find('[data-test="manual-account-result-42"]').exists()).toBe(false)
    expect(post).not.toHaveBeenCalled()
  })

  it('keeps an incomplete decision index error non-dismissible and manual mutations disabled', async () => {
    const nextDecisionsEndpoint = `${MANUAL_DECISIONS_ENDPOINT}?cursor=next`
    get.mockImplementation(async (path: string) => {
      if (path === ACCESS_ENDPOINT) return OPEN_POLICY_WORKSPACE
      if (path.split('?')[0] === ACCOUNTS_ENDPOINT) return ACCOUNTS_PAGE
      if (path === MANUAL_DECISIONS_ENDPOINT) {
        return { items: [ALICE_DECISION], nextCursor: 'next' }
      }
      if (path === nextDecisionsEndpoint) throw { code: 'network_error' }
      throw new Error(`Unexpected GET ${path}`)
    })

    const wrapper = mountWorkspace()
    await flushPromises()

    const search = wrapper.get('[data-test="manual-account-search"]')
    expect(search.attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-test="error-dismiss"]').exists()).toBe(false)

    expect(search.attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-test="manual-account-result-42"]').exists()).toBe(false)
    expect(post).not.toHaveBeenCalled()
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
