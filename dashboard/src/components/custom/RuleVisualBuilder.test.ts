import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import type { Rule } from '@/lib/appAccess'
import RuleVisualBuilder from './RuleVisualBuilder.vue'

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

function mountBuilder(
  modelValue: Rule,
  limits: Partial<{ maxDepth: number; maxNodes: number; maxChildren: number }> = {},
  issues: Array<{ path: string; reason: string; messageKey: string }> = [],
) {
  let wrapper: VueWrapper
  wrapper = mount(RuleVisualBuilder, {
    props: {
      modelValue,
      providers: PROVIDERS,
      issues,
      maxDepth: limits.maxDepth ?? 8,
      maxNodes: limits.maxNodes ?? 64,
      maxChildren: limits.maxChildren ?? 32,
      'onUpdate:modelValue': (next: Rule) => wrapper.setProps({ modelValue: next }),
    },
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })
  mounted.push(wrapper)
  return wrapper
}

function lastUpdate(wrapper: VueWrapper): Rule {
  return wrapper.emitted('update:modelValue')!.at(-1)![0] as Rule
}

async function openPredicateActions(wrapper: VueWrapper, path: string) {
  await wrapper.get(`[data-test="predicate-actions-${path}"]`).trigger('click')
  await flushPromises()
}

afterEach(() => {
  while (mounted.length) mounted.pop()!.unmount()
  document.body.innerHTML = ''
})

describe('RuleVisualBuilder', () => {
  it.each([
    {
      label: 'persisted root leaf',
      rule: { version: 1, condition: { fact: 'login_method', method: 'passkey' } } as Rule,
      selector: '[data-test="predicate-row-root"]',
    },
    {
      label: 'incomplete new root leaf',
      rule: { version: 1, condition: {} } as Rule,
      selector: '[data-test="predicate-row-root"]',
    },
    {
      label: 'persisted root group',
      rule: { version: 1, condition: { op: 'all', children: [{}] } } as Rule,
      selector: '[data-test="rule-group-root"]',
    },
  ])('renders a $label', ({ rule, selector }) => {
    const wrapper = mountBuilder(rule)
    expect(wrapper.find(selector).exists()).toBe(true)
  })

  it('wraps a root leaf in ALL, focuses the new condition, and announces the addition', async () => {
    const original: Rule = { version: 1, condition: { fact: 'avatar', source: 'user_uploaded' } }
    const wrapper = mountBuilder(original)

    await wrapper.get('[data-test="root-add-condition"]').trigger('click')
    await flushPromises()

    expect(lastUpdate(wrapper)).toEqual({
      version: 1,
      condition: {
        op: 'all',
        children: [{ fact: 'avatar', source: 'user_uploaded' }, {}],
      },
    })
    expect(original).toEqual({ version: 1, condition: { fact: 'avatar', source: 'user_uploaded' } })
    expect(document.activeElement).toBe(wrapper.get('[data-test="predicate-fact-root-1"]').element)
    expect(wrapper.emitted('announce')?.at(-1)?.[0]).toBe('Condition added.')
    expect(wrapper.get('[data-test="rule-builder-live"]').text()).toBe('Condition added.')
  })

  it('adds a nested group immutably, focuses its first condition, and announces it', async () => {
    const original: Rule = {
      version: 1,
      condition: { op: 'all', children: [{ fact: 'avatar', source: 'any' }] },
    }
    const wrapper = mountBuilder(original)

    await wrapper.get('[data-test="group-add-group-root"]').trigger('click')
    await flushPromises()

    expect(lastUpdate(wrapper)).toEqual({
      version: 1,
      condition: {
        op: 'all',
        children: [
          { fact: 'avatar', source: 'any' },
          { op: 'all', children: [{}] },
        ],
      },
    })
    expect(document.activeElement).toBe(wrapper.get('[data-test="predicate-fact-root-1-0"]').element)
    expect(wrapper.emitted('announce')?.at(-1)?.[0]).toBe('Nested group added.')
  })

  it('removes one subtree, focuses its adjacent condition, and restores it with one-level Undo', async () => {
    const original: Rule = {
      version: 1,
      condition: {
        op: 'all',
        children: [
          { fact: 'avatar', source: 'any' },
          { fact: 'login_method', method: 'passkey' },
          { fact: 'connection.protocol', protocol: 'oidc' },
        ],
      },
    }
    const wrapper = mountBuilder(original)

    await openPredicateActions(wrapper, 'root-1')
    const remove = document.body.querySelector<HTMLElement>('[data-test="predicate-remove-root-1"]')!
    remove.dispatchEvent(new Event('click', { bubbles: true }))
    await flushPromises()

    expect(lastUpdate(wrapper)).toEqual({
      version: 1,
      condition: {
        op: 'all',
        children: [
          { fact: 'avatar', source: 'any' },
          { fact: 'connection.protocol', protocol: 'oidc' },
        ],
      },
    })
    expect(document.activeElement).toBe(wrapper.get('[data-test="predicate-fact-root-1"]').element)
    expect(wrapper.emitted('announce')?.at(-1)?.[0]).toBe('Condition removed.')

    await wrapper.get('[data-test="rule-builder-undo"]').trigger('click')
    await flushPromises()
    expect(lastUpdate(wrapper)).toEqual(original)
    expect(document.activeElement).toBe(wrapper.get('[data-test="predicate-fact-root-1"]').element)
    expect(wrapper.emitted('announce')?.at(-1)?.[0]).toBe('Removed item restored.')
    expect(wrapper.find('[data-test="rule-builder-undo"]').exists()).toBe(false)
  })

  it('moves a condition, follows it with focus, and announces the direction', async () => {
    const wrapper = mountBuilder({
      version: 1,
      condition: {
        op: 'all',
        children: [
          { fact: 'avatar', source: 'any' },
          { fact: 'login_method', method: 'passkey' },
        ],
      },
    })

    await openPredicateActions(wrapper, 'root-1')
    const moveUp = document.body.querySelector<HTMLElement>('[data-test="predicate-move-up-root-1"]')!
    moveUp.dispatchEvent(new Event('click', { bubbles: true }))
    await flushPromises()

    expect(lastUpdate(wrapper)).toEqual({
      version: 1,
      condition: {
        op: 'all',
        children: [
          { fact: 'login_method', method: 'passkey' },
          { fact: 'avatar', source: 'any' },
        ],
      },
    })
    expect(document.activeElement).toBe(wrapper.get('[data-test="predicate-fact-root-0"]').element)
    expect(wrapper.emitted('announce')?.at(-1)?.[0]).toBe('Condition moved up.')
  })

  it('explains why a root leaf cannot be wrapped when the node limit is reached', () => {
    const wrapper = mountBuilder(
      { version: 1, condition: { fact: 'avatar', source: 'any' } },
      { maxNodes: 2 },
    )
    const button = wrapper.get<HTMLButtonElement>('[data-test="root-add-condition"]')
    const reasonId = button.attributes('aria-describedby')

    expect(button.element.disabled).toBe(true)
    expect(wrapper.get(`#${reasonId}`).text()).toBe('This rule cannot contain more than 2 conditions and groups.')
  })
  it('does not wrap a negative root leaf beyond the depth limit', () => {
    const wrapper = mountBuilder(
      { version: 1, condition: { op: 'not', child: { fact: 'avatar', source: 'any' } } },
      { maxDepth: 2 },
    )
    const button = wrapper.get<HTMLButtonElement>('[data-test="root-add-condition"]')
    const reasonId = button.attributes('aria-describedby')

    expect(button.element.disabled).toBe(true)
    expect(wrapper.get(`#${reasonId}`).text()).toBe('Nested groups cannot go deeper than 2.')
  })

})
