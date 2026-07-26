import { afterEach, describe, expect, it } from 'vitest'
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
  option!.dispatchEvent(new MouseEvent('pointerup', { bubbles: true, button: 0 }))
  await flushPromises()
}

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
})
