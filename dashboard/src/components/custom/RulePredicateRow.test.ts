import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import type { Rule } from '@/lib/appAccess'
import type { RuleValidationIssue } from '@/lib/ruleDraft'
import RulePredicateRow from './RulePredicateRow.vue'

const PROVIDERS = [
  { slug: 'corporate', displayName: 'Corporate identity' },
  { slug: 'partners', displayName: 'Partner directory' },
]
const mounted: VueWrapper[] = []

const i18n = () => createI18n({
  legacy: false,
  locale: 'en',
  fallbackLocale: 'en',
  messages: { en },
})

function mountRow(
  rule: Rule,
  options: {
    issues?: RuleValidationIssue[]
    canMoveUp?: boolean
    canMoveDown?: boolean
    canRemove?: boolean
    maxDepth?: number
    maxNodes?: number
  } = {},
) {
  const wrapper = mount(RulePredicateRow, {
    props: {
      rule,
      path: [],
      providers: PROVIDERS,
      issues: options.issues ?? [],
      canMoveUp: options.canMoveUp ?? false,
      canMoveDown: options.canMoveDown ?? false,
      canRemove: options.canRemove ?? false,
      maxDepth: options.maxDepth ?? 8,
      maxNodes: options.maxNodes ?? 64,
    },
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })
  mounted.push(wrapper)
  return wrapper
}

async function choose(wrapper: VueWrapper, control: 'fact' | 'polarity' | 'value', value: string) {
  await wrapper.get(`[data-test="predicate-${control}-root"]`).trigger('keydown', { key: 'Enter' })
  await flushPromises()
  const option = document.body.querySelector<HTMLElement>(
    `[data-test="predicate-${control}-option-root-${value}"]`,
  )
  expect(option).not.toBeNull()
  option!.dispatchEvent(control === 'polarity'
    ? new Event('click', { bubbles: true })
    : new MouseEvent('pointerup', { bubbles: true, button: 0 }))
  await flushPromises()
}

beforeAll(() => {
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(() => {
  while (mounted.length) mounted.pop()!.unmount()
  document.body.innerHTML = ''
})

describe('RulePredicateRow', () => {
  it('offers only the four fact kinds and keeps the clause in fact, polarity, value order', async () => {
    const wrapper = mountRow({ version: 1, condition: { fact: 'avatar', source: 'any' } })

    const controls = wrapper.findAll('[data-clause-control]')
    expect(controls.map((control) => control.attributes('data-clause-control'))).toEqual([
      'fact',
      'polarity',
      'value',
    ])

    await wrapper.get('[data-test="predicate-fact-root"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const options = Array.from(document.body.querySelectorAll<HTMLElement>('[data-test^="predicate-fact-option-root-"]'))
    expect(options.map((option) => option.getAttribute('data-value'))).toEqual([
      'connection.provider',
      'connection.protocol',
      'login_method',
      'avatar',
    ])
    expect(options.map((option) => option.textContent?.trim()).join(' ')).not.toMatch(/\b(?:ALL|ANY|NOT)\b/)
    expect(wrapper.get('[data-test="predicate-polarity-root"]').text()).toContain('is')
    expect(wrapper.get('[data-test="predicate-fact-root"]').text()).toContain('Avatar')
    expect(wrapper.get('[data-test="predicate-value-root"]').text()).toContain('Any available avatar')
  })

  it('uses the compact two-column flow below md and the approved four-column measure at md', () => {
    const wrapper = mountRow(
      { version: 1, condition: { fact: 'avatar', source: 'any' } },
      { canRemove: true },
    )
    const grid = wrapper.get('[data-test="predicate-layout-root"]')
    const fact = wrapper.get('[data-test="predicate-fact-root"]')
    const polarity = wrapper.get('[data-test="predicate-polarity-root"]')
    const value = wrapper.get('[data-test="predicate-value-root"]')
    const actions = wrapper.get('[data-test="predicate-actions-root"]')

    expect(grid.classes()).toEqual(expect.arrayContaining([
      'grid-cols-[minmax(0,1fr)_auto]',
      'md:grid-cols-[minmax(150px,1fr)_auto_minmax(180px,1.15fr)_auto]',
      'md:items-start',
    ]))
    expect(grid.classes().some((className) => className.startsWith('sm:grid-cols-'))).toBe(false)
    expect(fact.classes()).toEqual(expect.arrayContaining(['col-start-1', 'row-start-1', 'md:col-auto', 'md:row-auto']))
    expect(actions.classes()).toEqual(expect.arrayContaining(['col-start-2', 'row-start-1', 'md:col-auto', 'md:row-auto']))
    expect(polarity.classes()).toEqual(expect.arrayContaining(['col-start-1', 'row-start-2', 'md:col-auto', 'md:row-auto']))
    expect(value.classes()).toEqual(expect.arrayContaining(['col-start-1', 'col-end-2', 'row-start-3', 'md:col-auto', 'md:row-auto']))
  })

  it('shows provider display names with their persisted slugs', async () => {
    const wrapper = mountRow({
      version: 1,
      condition: { fact: 'connection.provider', provider: 'corporate' },
    })

    await wrapper.get('[data-test="predicate-value-root"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()

    expect(document.body.textContent).toContain('Corporate identity')
    expect(document.body.textContent).toContain('corporate')
    expect(document.body.textContent).toContain('Partner directory')
    expect(document.body.textContent).toContain('partners')
  })

  it('links an incomplete control to its exact validation message', () => {
    const issues: RuleValidationIssue[] = [{
      path: '$.condition',
      reason: 'invalid_shape',
      messageKey: 'manage.policy.rule.validation.invalid_shape',
    }]
    const wrapper = mountRow({ version: 1, condition: {} }, { issues })
    const fact = wrapper.get('[data-test="predicate-fact-root"]')
    const error = wrapper.get('[data-test="predicate-error-root"]')

    expect(fact.attributes('aria-invalid')).toBe('true')
    expect(fact.attributes('aria-describedby')).toBe(error.attributes('id'))
    expect(error.attributes('role')).toBe('alert')
    expect(error.text()).toBe('Choose a condition type and value.')
  })

  it('links an exact value-field issue to the value control', () => {
    const issues: RuleValidationIssue[] = [{
      path: '$.condition.provider',
      reason: 'missing_provider',
      messageKey: 'manage.policy.rule.validation.missing_provider',
    }]
    const wrapper = mountRow({ version: 1, condition: { fact: 'connection.provider' } }, { issues })
    const value = wrapper.get('[data-test="predicate-value-root"]')
    const error = wrapper.get('[data-test="predicate-error-root"]')

    expect(value.attributes('aria-invalid')).toBe('true')
    expect(value.attributes('aria-describedby')).toBe(error.attributes('id'))
    expect(error.text()).toBe('Choose a connection provider.')
  })

  it.each([
    { label: 'node', options: { maxNodes: 1 }, message: 'This rule cannot contain more than 1 conditions and groups.' },
    { label: 'depth', options: { maxDepth: 1 }, message: 'Nested groups cannot go deeper than 1.' },
  ])('disables is not when the $label limit cannot fit its NOT wrapper', async ({ options, message }) => {
    const wrapper = mountRow(
      { version: 1, condition: { fact: 'avatar', source: 'any' } },
      options,
    )

    await wrapper.get('[data-test="predicate-polarity-root"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const negative = document.body.querySelector<HTMLElement>('[data-test="predicate-polarity-option-root-negative"]')

    expect(negative?.hasAttribute('data-disabled')).toBe(true)
    expect(negative?.textContent).toContain(message)
  })

  it('changes polarity immutably while preserving the selected fact and value', async () => {
    const original: Rule = {
      version: 1,
      condition: { fact: 'connection.protocol', protocol: 'oidc' },
    }
    const wrapper = mountRow(original)

    await choose(wrapper, 'polarity', 'negative')

    const update = wrapper.emitted('update:rule')?.at(-1)?.[0] as Rule
    expect(update).toEqual({
      version: 1,
      condition: { op: 'not', child: { fact: 'connection.protocol', protocol: 'oidc' } },
    })
    expect(update).not.toBe(original)
    expect(original).toEqual({
      version: 1,
      condition: { fact: 'connection.protocol', protocol: 'oidc' },
    })
  })

  it('emits an immutable fact update from a keyboard selection', async () => {
    const original: Rule = { version: 1, condition: { fact: 'avatar', source: 'any' } }
    const wrapper = mountRow(original)

    await choose(wrapper, 'fact', 'login_method')

    const update = wrapper.emitted('update:rule')?.at(-1)?.[0] as Rule
    expect(update).toEqual({ version: 1, condition: { fact: 'login_method' } })
    expect(update).not.toBe(original)
    expect(original).toEqual({ version: 1, condition: { fact: 'avatar', source: 'any' } })
  })

  it('renders polarity as an unframed connective trigger with keyboard selection', async () => {
    const wrapper = mountRow({ version: 1, condition: { fact: 'avatar', source: 'any' } })
    const trigger = wrapper.get('[data-test="predicate-polarity-root"]')

    expect(trigger.attributes('data-slot')).toBe('dropdown-menu-trigger')
    expect(trigger.classes()).toEqual(expect.arrayContaining(['border-0', 'bg-transparent']))
    expect(trigger.classes()).not.toContain('border-input')

    trigger.element.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()
    expect(document.body.textContent).toContain('is not')
    document.body.querySelector<HTMLElement>('[data-test="predicate-polarity-option-root-negative"]')!
      .dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()

    expect(wrapper.emitted('update:rule')?.at(-1)?.[0]).toEqual({
      version: 1,
      condition: { op: 'not', child: { fact: 'avatar', source: 'any' } },
    })
  })

  it('opens the predicate ellipsis menu on click with visible actions from a direct trigger', async () => {
    const wrapper = mountRow(
      { version: 1, condition: { fact: 'avatar', source: 'user_uploaded' } },
      { canMoveUp: true, canMoveDown: true, canRemove: true },
    )
    const trigger = wrapper.get('[data-test="predicate-actions-root"]')

    expect(trigger.attributes('data-slot')).toBe('dropdown-menu-trigger')
    expect(trigger.find('[data-slot="tooltip-trigger"]').exists()).toBe(false)
    await trigger.trigger('click')
    await flushPromises()

    const menu = document.body.querySelector<HTMLElement>('[data-slot="dropdown-menu-content"]')
    expect(menu).not.toBeNull()
    expect(menu!.textContent).toContain('Move up')
    expect(menu!.textContent).toContain('Move down')
    expect(menu!.textContent).toContain('Remove condition')
  })

  it('uses condition-specific accessible names for move and remove commands', async () => {
    const wrapper = mountRow(
      { version: 1, condition: { fact: 'avatar', source: 'user_uploaded' } },
      { canMoveUp: true, canMoveDown: true, canRemove: true },
    )

    await wrapper.get('[data-test="predicate-actions-root"]').trigger('click')
    await flushPromises()

    const moveUp = document.body.querySelector<HTMLElement>('[data-test="predicate-move-up-root"]')
    const moveDown = document.body.querySelector<HTMLElement>('[data-test="predicate-move-down-root"]')
    const remove = document.body.querySelector<HTMLElement>('[data-test="predicate-remove-root"]')
    expect(moveUp?.getAttribute('aria-label')).toContain('User-uploaded avatar')
    expect(moveDown?.getAttribute('aria-label')).toContain('User-uploaded avatar')
    expect(remove?.getAttribute('aria-label')).toContain('User-uploaded avatar')

    remove!.dispatchEvent(new Event('click', { bubbles: true }))
    await flushPromises()
    expect(wrapper.emitted('remove')?.at(-1)?.[0]).toEqual([])
  })
  it('returns focus to the kebab trigger when its menu is cancelled', async () => {
    const wrapper = mountRow(
      { version: 1, condition: { fact: 'avatar', source: 'user_uploaded' } },
      { canRemove: true },
    )
    const trigger = wrapper.get<HTMLButtonElement>('[data-test="predicate-actions-root"]')

    await trigger.trigger('click')
    await flushPromises()
    const content = document.body.querySelector<HTMLElement>('[data-slot="dropdown-menu-content"]')
    expect(content).not.toBeNull()
    content!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()

    expect(document.activeElement).toBe(trigger.element)
  })

})
