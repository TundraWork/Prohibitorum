import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import type { Rule } from '@/lib/appAccess'
import type { RuleValidationIssue } from '@/lib/ruleDraft'
import RuleGroupEditor from './RuleGroupEditor.vue'

const PROVIDERS = [{ slug: 'corporate', displayName: 'Corporate identity' }]
const mounted: VueWrapper[] = []
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

beforeAll(() => {
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

function mountGroup(
  rule: Rule,
  options: Partial<{
    issues: RuleValidationIssue[]
    maxDepth: number
    maxNodes: number
    maxChildren: number
    canRemove: boolean
    canMoveUp: boolean
    canMoveDown: boolean
  }> = {},
) {
  const wrapper = mount(RuleGroupEditor, {
    props: {
      rule,
      path: [],
      providers: PROVIDERS,
      issues: options.issues ?? [],
      maxDepth: options.maxDepth ?? 8,
      maxNodes: options.maxNodes ?? 64,
      maxChildren: options.maxChildren ?? 32,
      canRemove: options.canRemove ?? false,
      canMoveUp: options.canMoveUp ?? false,
      canMoveDown: options.canMoveDown ?? false,
    },
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })
  mounted.push(wrapper)
  return wrapper
}

afterEach(() => {
  while (mounted.length) mounted.pop()!.unmount()
  document.body.innerHTML = ''
})

describe('RuleGroupEditor', () => {
  it('renders ALL and ANY rails with exact descriptions and explicit connectors', () => {
    const wrapper = mountGroup({
      version: 1,
      condition: {
        op: 'all',
        children: [
          { fact: 'avatar', source: 'any' },
          {
            op: 'any',
            children: [
              { fact: 'login_method', method: 'passkey' },
              { fact: 'connection.protocol', protocol: 'oidc' },
            ],
          },
        ],
      },
    })

    expect(wrapper.get('[data-test="group-mode-root"]').text()).toContain('ALL')
    expect(wrapper.get('[data-test="group-description-root"]').text()).toBe('Every condition in this group must be true.')
    expect(wrapper.get('[data-test="group-mode-root-1"]').text()).toContain('ANY')
    expect(wrapper.get('[data-test="group-description-root-1"]').text()).toBe('At least one condition in this group must be true.')
    expect(wrapper.get('[data-test="connector-root-1"]').text()).toBe('AND')
    expect(wrapper.get('[data-test="connector-root-1-1"]').text()).toBe('OR')
    expect(wrapper.findAll('[data-test^="group-mode-not-"]')).toHaveLength(0)
  })

  it('gives each predicate control its sibling position and parent group mode', () => {
    const wrapper = mountGroup({
      version: 1,
      condition: {
        op: 'any',
        children: [
          { fact: 'avatar', source: 'any' },
          { fact: 'login_method', method: 'passkey' },
        ],
      },
    })

    expect(wrapper.get('[data-test="predicate-fact-root-0"]').attributes('aria-label')).toBe('Condition type for condition 1 of 2 in ANY group')
    expect(wrapper.get('[data-test="predicate-polarity-root-0"]').attributes('aria-label')).toBe('Comparison for condition 1 of 2 in ANY group')
    expect(wrapper.get('[data-test="predicate-value-root-0"]').attributes('aria-label')).toBe('Value for condition 1 of 2 in ANY group')
    expect(wrapper.get('[data-test="predicate-fact-root-1"]').attributes('aria-label')).toBe('Condition type for condition 2 of 2 in ANY group')
  })

  it('uses one slim unrounded structural rail for each group', () => {
    const wrapper = mountGroup({
      version: 1,
      condition: {
        op: 'all',
        children: [{ op: 'any', children: [{ fact: 'avatar', source: 'any' }] }],
      },
    })

    const groups = wrapper.findAll('[data-rule-group]')
    const rails = wrapper.findAll('[data-group-rail]')
    expect(groups).toHaveLength(2)
    expect(rails).toHaveLength(2)
    for (const group of groups) {
      expect(group.classes().some((className) => className.startsWith('rounded'))).toBe(false)
      expect(group.classes()).toContain('pl-5')
    }
  })

  it('starts the structural rail below the scope header and ends it near the group footer', () => {
    const wrapper = mountGroup({
      version: 1,
      condition: { op: 'all', children: [{ fact: 'avatar', source: 'any' }] },
    })

    const rail = wrapper.get('[data-group-rail]')
    expect(rail.classes()).toEqual(expect.arrayContaining(['top-[30px]', 'bottom-[5px]']))
    expect(rail.classes()).not.toContain('inset-y-2')
  })

  it('anchors one compact ALL or ANY control to each scope rail and supports keyboard selection', async () => {
    const wrapper = mountGroup({
      version: 1,
      condition: {
        op: 'all',
        children: [{ op: 'any', children: [{ fact: 'avatar', source: 'any' }] }],
      },
    })

    for (const pathKey of ['root', 'root-0']) {
      const control = wrapper.get(`[data-test="group-mode-${pathKey}"]`)
      expect(control.attributes('data-slot')).toBe('dropdown-menu-trigger')
      expect(control.classes()).toEqual(expect.arrayContaining(['inline-flex', 'w-auto']))
      expect(control.classes()).not.toContain('w-full')
    }

    const rootControl = wrapper.get('[data-test="group-mode-root"]')
    await rootControl.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const anyOption = document.body.querySelector<HTMLElement>('[data-test="group-mode-any-root"]')
    expect(anyOption).not.toBeNull()
    anyOption!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()

    expect(wrapper.emitted('update:rule')?.at(-1)?.[0]).toEqual({
      version: 1,
      condition: {
        op: 'any',
        children: [{ op: 'any', children: [{ fact: 'avatar', source: 'any' }] }],
      },
    })
  })

  it('opens every nested group ellipsis menu on click with visible actions', async () => {
    const wrapper = mountGroup({
      version: 1,
      condition: {
        op: 'all',
        children: [{ op: 'any', children: [{ fact: 'avatar', source: 'any' }] }],
      },
    }, { canRemove: true })

    const triggers = wrapper.findAll('[data-test^="group-actions-"]')
    expect(triggers).toHaveLength(2)
    for (const trigger of triggers) {
      expect(trigger.attributes('data-slot')).toBe('dropdown-menu-trigger')
      expect(trigger.find('[data-slot="tooltip-trigger"]').exists()).toBe(false)
      await trigger.trigger('click')
      await flushPromises()
      expect(document.body.querySelector('[data-slot="dropdown-menu-content"]')).not.toBeNull()
      expect(document.body.textContent).toContain('Remove condition')
      document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      await flushPromises()
    }
  })
  it('switches ALL to ANY without changing any child or mutating the source', async () => {
    const original: Rule = {
      version: 1,
      condition: {
        op: 'all',
        children: [
          { fact: 'avatar', source: 'user_uploaded' },
          { op: 'not', child: { fact: 'login_method', method: 'passkey' } },
        ],
      },
    }
    const wrapper = mountGroup(original)

    await wrapper.get('[data-test="group-mode-root"]').trigger('click')
    await flushPromises()
    document.body.querySelector<HTMLElement>('[data-test="group-mode-any-root"]')!
      .dispatchEvent(new Event('click', { bubbles: true }))
    await flushPromises()

    const update = wrapper.emitted('update:rule')?.at(-1)?.[0] as Rule
    expect(update).toEqual({
      version: 1,
      condition: {
        op: 'any',
        children: [
          { fact: 'avatar', source: 'user_uploaded' },
          { op: 'not', child: { fact: 'login_method', method: 'passkey' } },
        ],
      },
    })
    expect(update).not.toBe(original)
    expect(original.condition.op).toBe('all')
  })

  it('renders negative leaves as is not and preserves their value when polarity changes', async () => {
    const wrapper = mountGroup({
      version: 1,
      condition: {
        op: 'all',
        children: [{ op: 'not', child: { fact: 'avatar', source: 'user_uploaded' } }],
      },
    })
    expect(wrapper.get('[data-test="predicate-polarity-root-0"]').text()).toContain('is not')

    await wrapper.get('[data-test="predicate-polarity-root-0"]').trigger('click')
    await flushPromises()
    const positive = document.body.querySelector<HTMLElement>('[data-test="predicate-polarity-option-root-0-positive"]')
    positive!.dispatchEvent(new Event('click', { bubbles: true }))
    await flushPromises()

    expect(wrapper.emitted('update:rule')?.at(-1)?.[0]).toEqual({
      version: 1,
      condition: { op: 'all', children: [{ fact: 'avatar', source: 'user_uploaded' }] },
    })
  })

  it('rejects a NOT-wrapped group with the shared exact error instead of rendering it as a group', () => {
    const issues: RuleValidationIssue[] = [{
      path: '$.condition.child',
      reason: 'not_requires_fact',
      messageKey: 'manage.policy.rule.validation.not_requires_fact',
    }]
    const wrapper = mountGroup({
      version: 1,
      condition: { op: 'not', child: { op: 'all', children: [{ fact: 'avatar', source: 'any' }] } },
    }, { issues })

    expect(wrapper.find('[data-rule-group]').exists()).toBe(false)
    expect(wrapper.find('[data-test="predicate-row-root"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="invalid-node-root"]').text()).toBe('Is not can only apply to one condition, not a group.')
    expect(wrapper.get('[data-test="invalid-node-root"]').attributes('role')).toBe('alert')
  })

  it('shows the shared NOT error at a nested child path without adding a NOT rail', () => {
    const issues: RuleValidationIssue[] = [{
      path: '$.condition.children[0].child',
      reason: 'not_requires_fact',
      messageKey: 'manage.policy.rule.validation.not_requires_fact',
    }]
    const wrapper = mountGroup({
      version: 1,
      condition: {
        op: 'all',
        children: [{ op: 'not', child: { op: 'any', children: [{ fact: 'avatar', source: 'any' }] } }],
      },
    }, { issues })

    expect(wrapper.get('[data-test="invalid-node-root-0"]').text()).toBe('Is not can only apply to one condition, not a group.')
    expect(wrapper.find('[data-test="group-mode-not-root-0"]').exists()).toBe(false)
  })

  it('renders the exact issue for an invalid nested child field', () => {
    const issues: RuleValidationIssue[] = [{
      path: '$.condition.children[0].provider',
      reason: 'missing_provider',
      messageKey: 'manage.policy.rule.validation.missing_provider',
    }]
    const wrapper = mountGroup({
      version: 1,
      condition: { op: 'all', children: [{ fact: 'connection.provider' }] },
    }, { issues })

    expect(wrapper.get('[data-test="predicate-error-root-0"]').text()).toBe('Choose a connection provider.')
  })

  it('offers only Add condition and Add nested group and emits their parent path', async () => {
    const wrapper = mountGroup({
      version: 1,
      condition: { op: 'all', children: [{ fact: 'avatar', source: 'any' }] },
    })

    const addCondition = wrapper.get('[data-test="group-add-condition-root"]')
    const addGroup = wrapper.get('[data-test="group-add-group-root"]')
    expect(addCondition.text()).toContain('Add condition')
    expect(addGroup.text()).toContain('Add nested group')
    expect(wrapper.text()).not.toContain('Leaf')
    expect(wrapper.find('[data-test^="group-add-not-"]').exists()).toBe(false)

    await addCondition.trigger('click')
    await addGroup.trigger('click')
    expect(wrapper.emitted('add-predicate')?.at(-1)?.[0]).toEqual([])
    expect(wrapper.emitted('add-group')?.at(-1)?.[0]).toEqual([])
  })

  it.each([
    {
      label: 'depth',
      rule: { version: 1, condition: { op: 'all', children: [{}] } } as Rule,
      limits: { maxDepth: 2, maxNodes: 64, maxChildren: 32 },
      selector: 'group-add-group-root',
      message: 'Nested groups cannot go deeper than 2.',
    },
    {
      label: 'node count',
      rule: { version: 1, condition: { op: 'all', children: [{}, {}] } } as Rule,
      limits: { maxDepth: 8, maxNodes: 3, maxChildren: 32 },
      selector: 'group-add-condition-root',
      message: 'This rule cannot contain more than 3 conditions and groups.',
    },
    {
      label: 'child count',
      rule: { version: 1, condition: { op: 'all', children: [{}] } } as Rule,
      limits: { maxDepth: 8, maxNodes: 64, maxChildren: 1 },
      selector: 'group-add-condition-root',
      message: 'A group cannot contain more than 1 conditions and groups.',
    },
  ])('disables an add action and links the $label explanation', ({ rule, limits, selector, message }) => {
    const wrapper = mountGroup(rule, limits)
    const button = wrapper.get<HTMLButtonElement>(`[data-test="${selector}"]`)
    const reasonId = button.attributes('aria-describedby')

    expect(button.element.disabled).toBe(true)
    expect(reasonId).toBeTruthy()
    expect(wrapper.get(`#${reasonId}`).text()).toBe(message)
  })

  it('emits named move and remove commands with the immutable group path', async () => {
    const wrapper = mountGroup({
      version: 1,
      condition: { op: 'all', children: [{ fact: 'avatar', source: 'any' }] },
    }, { canMoveUp: true, canMoveDown: true, canRemove: true })

    await wrapper.get('[data-test="group-actions-root"]').trigger('click')
    await flushPromises()
    const down = document.body.querySelector<HTMLElement>('[data-test="group-move-down-root"]')!
    down.dispatchEvent(new Event('click', { bubbles: true }))
    await flushPromises()
    expect(wrapper.emitted('move')?.at(-1)).toEqual([[], 1])

    await wrapper.get('[data-test="group-actions-root"]').trigger('click')
    await flushPromises()
    const remove = document.body.querySelector<HTMLElement>('[data-test="group-remove-root"]')!
    remove.dispatchEvent(new Event('click', { bubbles: true }))
    await flushPromises()
    expect(wrapper.emitted('remove')?.at(-1)?.[0]).toEqual([])
  })
})
