import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import type { Rule } from '@/lib/appAccess'
import RuleEditor, { type RuleEditorDraft } from './RuleEditor.vue'

const PROVIDERS = [{ slug: 'corporate', displayName: 'Corporate identity' }]
const EMPTY_RULE: Rule = { version: 1, condition: { op: 'all', children: [{}] } }
const PASSKEY_RULE: Rule = { version: 1, condition: { fact: 'login_method', method: 'passkey' } }
const INITIAL_DRAFT: RuleEditorDraft = {
  slug: '',
  displayName: '',
  description: '',
  exposedToDownstream: false,
  rule: EMPTY_RULE,
}
const EDIT_DRAFT: RuleEditorDraft = {
  slug: 'trusted-members',
  displayName: 'Trusted members',
  description: 'Verified access',
  exposedToDownstream: true,
  rule: PASSKEY_RULE,
}

const wrappers: VueWrapper[] = []
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

const GROUP_DRAFT: RuleEditorDraft = {
  slug: 'mixed-members',
  displayName: 'Mixed members',
  description: '',
  exposedToDownstream: false,
  rule: {
    version: 1,
    condition: {
      op: 'all',
      children: [
        { fact: 'login_method', method: 'passkey' },
        { fact: 'avatar', source: 'user_uploaded' },
      ],
    },
  },
}
beforeAll(() => {
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

function mountEditor(options: Partial<{
  initialDraft: RuleEditorDraft
  mode: 'create' | 'edit'
  busy: boolean
  serverError: { code: string }
}> = {}) {
  const wrapper = mount(RuleEditor, {
    props: {
      initialDraft: options.initialDraft ?? INITIAL_DRAFT,
      providers: PROVIDERS,
      previewEndpoint: '/managed-applications/oidc/wiki/rule-preview',
      busy: options.busy ?? false,
      serverError: options.serverError,
      mode: options.mode ?? 'create',
    },
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })
  wrappers.push(wrapper)
  return wrapper
}

async function setInput(wrapper: VueWrapper, testId: string, value: string) {
  await wrapper.get(`[data-test="${testId}"]`).setValue(value)
}

async function makeDraftValid(wrapper: VueWrapper) {
  await setInput(wrapper, 'rule-display-name', 'Passkey members')
  await wrapper.get('[data-test="predicate-fact-root-0"]').trigger('keydown', { key: 'Enter' })
  await flushPromises()
  const loginMethod = document.body.querySelector<HTMLElement>('[data-test="predicate-fact-option-root-0-login_method"]')!
  loginMethod.dispatchEvent(new MouseEvent('pointerup', { bubbles: true, button: 0 }))
  await flushPromises()
  await wrapper.get('[data-test="predicate-value-root-0"]').trigger('keydown', { key: 'Enter' })
  await flushPromises()
  document.body.querySelector<HTMLElement>('[data-test="predicate-value-option-root-0-passkey"]')!
    .dispatchEvent(new MouseEvent('pointerup', { bubbles: true, button: 0 }))
  await flushPromises()
}

afterEach(() => {
  while (wrappers.length) wrappers.pop()!.unmount()
  document.body.innerHTML = ''
})

describe('RuleEditor', () => {
  it('starts with an incomplete condition, shows Visual and JSON modes, and disables review', () => {
    const wrapper = mountEditor()

    expect(wrapper.get('[data-test="segment-visual"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="segment-json"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="predicate-error-root-0"]').text()).toContain('Choose a condition type and value.')
    expect(wrapper.get('[data-test="review-rule"]').attributes('disabled')).toBeDefined()
  })

  it('shares valid JSON with Visual mode and blocks a switch from invalid JSON while preserving the last valid rule', async () => {
    const wrapper = mountEditor({ initialDraft: EDIT_DRAFT, mode: 'edit' })
    await wrapper.get('[data-test="segment-json"]').trigger('click')
    const source = wrapper.get('[data-test="rule-json-source"]')
    await source.setValue(JSON.stringify({ version: 1, condition: { fact: 'avatar', source: 'user_uploaded' } }))
    await wrapper.get('[data-test="segment-visual"]').trigger('click')
    expect(wrapper.get('[data-test="predicate-value-root"]').text()).toContain('User-uploaded avatar')

    await wrapper.get('[data-test="segment-json"]').trigger('click')
    await wrapper.get('[data-test="rule-json-source"]').setValue('{broken')
    await wrapper.get('[data-test="segment-visual"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="rule-json-editor"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="rule-json-error"]').attributes('role')).toBe('alert')
    expect(document.activeElement).toBe(wrapper.get('[data-test="rule-json-source"]').element)

    await wrapper.get('[data-test="rule-json-source"]').setValue(JSON.stringify({ version: 1, condition: { fact: 'avatar', source: 'user_uploaded' } }))
    await wrapper.get('[data-test="segment-visual"]').trigger('click')
    expect(wrapper.get('[data-test="predicate-value-root"]').text()).toContain('User-uploaded avatar')
  })

  it('marks the retained impact preview stale while the visible JSON draft is invalid', async () => {
    const wrapper = mountEditor({ initialDraft: EDIT_DRAFT, mode: 'edit' })
    await wrapper.get('[data-test="segment-json"]').trigger('click')
    await wrapper.get('[data-test="rule-json-source"]').setValue('{broken')

    const preview = wrapper.getComponent({ name: 'RuleImpactPreview' })
    expect(preview.props('rule')).toEqual(PASSKEY_RULE)
    expect(preview.props('draftValid')).toBe(false)
  })

  it('treats JSON mode and an invalid JSON buffer as dirty and confirms cancellation', async () => {
    const wrapper = mountEditor({ initialDraft: EDIT_DRAFT, mode: 'edit' })
    await wrapper.get('[data-test="segment-json"]').trigger('click')
    expect(wrapper.emitted('dirty-change')?.at(-1)).toEqual([true])

    await wrapper.get('[data-test="rule-json-source"]').setValue('{broken')
    const unload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(unload)
    expect(unload.defaultPrevented).toBe(true)

    await wrapper.get('[data-test="cancel-rule"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('cancel')).toBeUndefined()
    expect(document.body.querySelector('[data-test="dirty-dialog"]')).not.toBeNull()
  })

  it('blocks Review and save while the visible JSON buffer is invalid', async () => {
    const wrapper = mountEditor({ initialDraft: EDIT_DRAFT, mode: 'edit' })
    await wrapper.get('[data-test="segment-json"]').trigger('click')
    await wrapper.get('[data-test="rule-json-source"]').setValue('{broken')

    const review = wrapper.get('[data-test="review-rule"]')
    expect(review.attributes('disabled')).toBeDefined()
    await review.trigger('click')
    expect(wrapper.find('[data-test="rule-review"]').exists()).toBe(false)
    expect(wrapper.emitted('save')).toBeUndefined()
  })

  it('preserves one structural Undo across a mode round trip and restores the exact subtree', async () => {
    const wrapper = mountEditor({ initialDraft: GROUP_DRAFT, mode: 'edit' })
    await wrapper.get('[data-test="predicate-actions-root-1"]').trigger('click')
    await flushPromises()
    document.body.querySelector<HTMLElement>('[data-test="predicate-remove-root-1"]')!
      .dispatchEvent(new Event('click', { bubbles: true }))
    await flushPromises()
    expect(wrapper.get('[data-test="rule-builder-undo"]').exists()).toBe(true)

    await wrapper.get('[data-test="segment-json"]').trigger('click')
    await wrapper.get('[data-test="segment-visual"]').trigger('click')
    await wrapper.get('[data-test="rule-builder-undo"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-test="segment-json"]').trigger('click')
    const restored = JSON.parse((wrapper.get('[data-test="rule-json-source"]').element as HTMLTextAreaElement).value)
    expect(restored).toEqual(GROUP_DRAFT.rule)
  })

  it('invalidates preserved structural Undo after a JSON transform', async () => {
    const wrapper = mountEditor({ initialDraft: GROUP_DRAFT, mode: 'edit' })
    await wrapper.get('[data-test="predicate-actions-root-1"]').trigger('click')
    await flushPromises()
    document.body.querySelector<HTMLElement>('[data-test="predicate-remove-root-1"]')!
      .dispatchEvent(new Event('click', { bubbles: true }))
    await flushPromises()
    await wrapper.get('[data-test="segment-json"]').trigger('click')
    await wrapper.get('[data-test="rule-json-source"]').setValue(JSON.stringify(PASSKEY_RULE))
    await wrapper.get('[data-test="segment-visual"]').trigger('click')

    expect(wrapper.find('[data-test="rule-builder-undo"]').exists()).toBe(false)
  })

  it('generates a slug from display name until the slug is manually edited', async () => {
    const wrapper = mountEditor()
    await setInput(wrapper, 'rule-display-name', 'Trusted Friends')
    await wrapper.get('[data-test="advanced-toggle"]').trigger('click')
    expect((wrapper.get('[data-test="rule-slug"]').element as HTMLInputElement).value).toBe('trusted-friends')

    await setInput(wrapper, 'rule-slug', 'friends-v2')
    await setInput(wrapper, 'rule-display-name', 'Renamed Friends')
    expect((wrapper.get('[data-test="rule-slug"]').element as HTMLInputElement).value).toBe('friends-v2')
  })

  it('keeps slug and claim exposure inside an accessible Advanced disclosure', async () => {
    const wrapper = mountEditor()
    const toggle = wrapper.get('[data-test="advanced-toggle"]')
    const panelId = toggle.attributes('aria-controls')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(wrapper.find(`#${panelId}`).exists()).toBe(false)

    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(wrapper.get(`#${panelId}`).find('[data-test="rule-slug"]').exists()).toBe(true)
    expect(wrapper.get(`#${panelId}`).find('[data-test="rule-exposed"]').exists()).toBe(true)
  })

  it('places live meaning and impact beside the builder with a narrow-screen stack', () => {
    const wrapper = mountEditor({ initialDraft: EDIT_DRAFT, mode: 'edit' })
    const layout = wrapper.get('[data-test="rule-editor-layout"]')
    expect(layout.classes()).toEqual(expect.arrayContaining(['grid-cols-1', 'min-[1536px]:grid-cols-[minmax(0,3fr)_minmax(17rem,2fr)]']))
    expect(layout.get('[data-test="editor-builder-column"]').exists()).toBe(true)
    expect(layout.get('[data-test="editor-insight-column"]').exists()).toBe(true)
    expect(layout.get('[data-test="rule-meaning"]').text()).toContain('Passkey')
    expect(layout.get('[data-test="rule-impact-preview"]').exists()).toBe(true)
  })

  it('moves to review without saving, then emits an exact closed draft only from review', async () => {
    const wrapper = mountEditor()
    await makeDraftValid(wrapper)
    expect(wrapper.get('[data-test="review-rule"]').attributes('disabled')).toBeUndefined()

    await wrapper.get('[data-test="review-rule"]').trigger('click')
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(wrapper.get('[data-test="rule-review"]').text()).toContain('Passkey members')
    expect(wrapper.get('[data-test="rule-review"]').text()).toContain('A matching account can access this app unless manually denied.')

    await wrapper.get('[data-test="save-rule"]').trigger('click')
    expect(wrapper.emitted('save')?.[0]?.[0]).toEqual({
      slug: 'passkey-members',
      displayName: 'Passkey members',
      description: '',
      exposedToDownstream: false,
      rule: { version: 1, condition: { op: 'all', children: [{ fact: 'login_method', method: 'passkey' }] } },
    })
  })

  it('requires explicit Save without preview confirmation after preview failure', async () => {
    const wrapper = mountEditor({ initialDraft: EDIT_DRAFT, mode: 'edit' })
    await wrapper.getComponent({ name: 'RuleImpactPreview' }).vm.$emit('state-change', 'error')
    await wrapper.get('[data-test="review-rule"]').trigger('click')
    await wrapper.get('[data-test="save-rule"]').trigger('click')
    await flushPromises()

    expect(wrapper.emitted('save')).toBeUndefined()
    const confirm = document.body.querySelector<HTMLElement>('[data-test="confirm-save-without-preview"]')!
    expect(confirm).toBeTruthy()
    confirm.click()
    await flushPromises()
    expect(wrapper.emitted('save')?.[0]?.[0]).toEqual(EDIT_DRAFT)
  })

  it('shows Saving and announces success with focused status after the save lifecycle', async () => {
    const wrapper = mountEditor({ initialDraft: EDIT_DRAFT, mode: 'edit' })
    await setInput(wrapper, 'rule-description', 'Updated before save')
    await wrapper.get('[data-test="review-rule"]').trigger('click')
    await wrapper.get('[data-test="save-rule"]').trigger('click')
    await wrapper.setProps({ busy: true })
    expect(wrapper.get('[data-test="save-rule"]').text()).toBe('Saving…')
    expect(wrapper.get('[data-test="save-rule"]').attributes('aria-busy')).toBe('true')

    await wrapper.setProps({ busy: false })
    await flushPromises()
    const status = wrapper.get('[data-test="rule-save-status"]')
    expect(status.text()).toBe('Rule group saved.')
    expect(status.attributes('role')).toBe('status')
    expect(document.activeElement).toBe(status.element)
    expect(wrapper.emitted('dirty-change')?.at(-1)).toEqual([false])
    const unload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(unload)
    expect(unload.defaultPrevented).toBe(false)
  })

  it('confirms dirty cancellation and installs beforeunload only while dirty', async () => {
    const wrapper = mountEditor()
    const unload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(unload)
    expect(unload.defaultPrevented).toBe(false)

    await setInput(wrapper, 'rule-display-name', 'Unsaved')
    expect(wrapper.emitted('dirty-change')?.at(-1)).toEqual([true])
    const dirtyUnload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(dirtyUnload)
    expect(dirtyUnload.defaultPrevented).toBe(true)

    await wrapper.get('[data-test="cancel-rule"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('cancel')).toBeUndefined()
    const safeAction = document.body.querySelector<HTMLElement>('[data-test="keep-editing"]')!
    expect(document.activeElement).toBe(safeAction)
    document.body.querySelector<HTMLElement>('[data-test="discard-draft"]')!.click()
    await flushPromises()
    expect(wrapper.emitted('cancel')).toBeTruthy()
  })

  it('preserves the draft and renders a server error beside the editor', async () => {
    const wrapper = mountEditor({ serverError: { code: 'server_error' } })
    await setInput(wrapper, 'rule-display-name', 'Still here')
    await wrapper.setProps({ serverError: { code: 'network_error' } })

    expect((wrapper.get('[data-test="rule-display-name"]').element as HTMLInputElement).value).toBe('Still here')
    expect(wrapper.get('[data-test="rule-server-error"]').attributes('role')).toBe('alert')
  })
})
