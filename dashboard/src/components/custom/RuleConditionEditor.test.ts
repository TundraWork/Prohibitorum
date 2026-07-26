import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import type { Condition } from '@/lib/appAccess'
import RuleConditionEditor from './RuleConditionEditor.vue'

const PROVIDERS = [{ slug: 'corporate' }, { slug: 'partners' }]
const mounted: VueWrapper[] = []

const i18n = () =>
  createI18n({
    legacy: false,
    locale: 'en',
    fallbackLocale: 'en',
    messages: { en },
  })

function mountEditor(
  modelValue: Condition,
  limits: Partial<{ maxDepth: number; maxNodes: number; maxChildren: number }> = {},
) {
  const wrapper = mount(RuleConditionEditor, {
    props: { modelValue, providers: PROVIDERS, ...limits },
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })
  mounted.push(wrapper)
  return wrapper
}

function lastUpdate(wrapper: VueWrapper): Condition {
  const updates = wrapper.emitted('update:modelValue') as Condition[][] | undefined
  expect(updates).toBeTruthy()
  return updates!.at(-1)![0]!
}

async function choose(
  wrapper: VueWrapper,
  control: 'kind' | 'value',
  path: string,
  value: string,
) {
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

async function openOptions(wrapper: VueWrapper, control: 'kind' | 'value', path: string) {
  const trigger = wrapper.get(`[data-test="condition-${control}-${path}"]`)
  if (!(trigger.element instanceof HTMLSelectElement)) {
    await trigger.trigger('keydown', { key: 'Enter' })
    await flushPromises()
  }
}

function expectAllAddControlsDisabled(wrapper: VueWrapper, path: string) {
  for (const kind of ['leaf', 'all', 'any', 'not']) {
    const button = wrapper.get<HTMLButtonElement>(`[data-test="add-${kind}-${path}"]`)
    expect(button.element.disabled).toBe(true)
  }
}

afterEach(() => {
  while (mounted.length > 0) mounted.pop()!.unmount()
  document.body.innerHTML = ''
})

describe('RuleConditionEditor', () => {
  const addCases: Array<{
    label: string
    selector: string
    expected: Condition
  }> = [
    {
      label: 'a leaf',
      selector: 'add-leaf-root',
      expected: {
        op: 'all',
        children: [
          { fact: 'login_method', method: 'passkey' },
          { fact: 'avatar', source: 'any' },
        ],
      },
    },
    {
      label: 'an all combinator',
      selector: 'add-all-root',
      expected: {
        op: 'all',
        children: [
          { fact: 'login_method', method: 'passkey' },
          { op: 'all', children: [] },
        ],
      },
    },
    {
      label: 'an any combinator',
      selector: 'add-any-root',
      expected: {
        op: 'all',
        children: [
          { fact: 'login_method', method: 'passkey' },
          { op: 'any', children: [] },
        ],
      },
    },
    {
      label: 'a not combinator',
      selector: 'add-not-root',
      expected: {
        op: 'all',
        children: [
          { fact: 'login_method', method: 'passkey' },
          { op: 'not', child: { fact: 'avatar', source: 'any' } },
        ],
      },
    },
  ]

  it.each(addCases)('adds $label with the correct AST shape instead of a lossy placeholder', async ({ selector, expected }) => {
    const wrapper = mountEditor({
      op: 'all',
      children: [{ fact: 'login_method', method: 'passkey' }],
    })

    await wrapper.get(`[data-test="${selector}"]`).trigger('click')

    expect(lastUpdate(wrapper)).toEqual(expected)
  })

  it('deep-clones every ancestor while nesting any and not instead of mutating the supplied tree', async () => {
    const initial: Condition = {
      op: 'all',
      children: [{ fact: 'login_method', method: 'passkey' }],
    }
    const wrapper = mountEditor(initial)

    await wrapper.get('[data-test="add-any-root"]').trigger('click')
    const withAny = lastUpdate(wrapper)
    expect(withAny).toEqual({
      op: 'all',
      children: [
        { fact: 'login_method', method: 'passkey' },
        { op: 'any', children: [] },
      ],
    })
    expect(withAny).not.toBe(initial)
    expect(withAny.children).not.toBe(initial.children)
    expect(withAny.children![0]).not.toBe(initial.children![0])
    expect(initial).toEqual({
      op: 'all',
      children: [{ fact: 'login_method', method: 'passkey' }],
    })

    await wrapper.setProps({ modelValue: withAny })
    await wrapper.get('[data-test="add-not-root-1"]').trigger('click')
    const withNot = lastUpdate(wrapper)
    expect(withNot).toEqual({
      op: 'all',
      children: [
        { fact: 'login_method', method: 'passkey' },
        {
          op: 'any',
          children: [{ op: 'not', child: { fact: 'avatar', source: 'any' } }],
        },
      ],
    })
    expect(withNot).not.toBe(withAny)
    expect(withNot.children).not.toBe(withAny.children)
    expect(withNot.children![1]).not.toBe(withAny.children![1])
    expect(withNot.children![1]!.children).not.toBe(withAny.children![1]!.children)
    expect(withAny).toEqual({
      op: 'all',
      children: [
        { fact: 'login_method', method: 'passkey' },
        { op: 'any', children: [] },
      ],
    })
  })

  it('removes only the addressed nested node instead of deleting or reordering its siblings', async () => {
    const initial: Condition = {
      op: 'all',
      children: [
        { fact: 'avatar', source: 'any' },
        {
          op: 'any',
          children: [
            { fact: 'connection.protocol', protocol: 'oidc' },
            { fact: 'login_method', method: 'federation' },
          ],
        },
      ],
    }
    const wrapper = mountEditor(initial)

    await wrapper.get('[data-test="remove-root-1-0"]').trigger('click')

    expect(lastUpdate(wrapper)).toEqual({
      op: 'all',
      children: [
        { fact: 'avatar', source: 'any' },
        {
          op: 'any',
          children: [{ fact: 'login_method', method: 'federation' }],
        },
      ],
    })
    expect(initial).toEqual({
      op: 'all',
      children: [
        { fact: 'avatar', source: 'any' },
        {
          op: 'any',
          children: [
            { fact: 'connection.protocol', protocol: 'oidc' },
            { fact: 'login_method', method: 'federation' },
          ],
        },
      ],
    })
  })

  it('switches between leaf and combinator shapes without retaining stale fields', async () => {
    const wrapper = mountEditor({ fact: 'avatar', source: 'user_uploaded' })

    await choose(wrapper, 'kind', 'root', 'all')
    const allRoot = lastUpdate(wrapper)
    expect(allRoot).toEqual({ op: 'all', children: [] })

    await wrapper.setProps({ modelValue: allRoot })
    await choose(wrapper, 'kind', 'root', 'connection.provider')
    expect(lastUpdate(wrapper)).toEqual({
      fact: 'connection.provider',
      provider: 'corporate',
    })
  })

  const factCases: Array<{
    label: string
    initial: Condition
    kind: NonNullable<Condition['fact']>
    expected: Condition
  }> = [
    {
      label: 'connection provider',
      initial: { fact: 'avatar', source: 'user_uploaded' },
      kind: 'connection.provider',
      expected: { fact: 'connection.provider', provider: 'corporate' },
    },
    {
      label: 'connection protocol',
      initial: { fact: 'avatar', source: 'user_uploaded' },
      kind: 'connection.protocol',
      expected: { fact: 'connection.protocol', protocol: 'oidc' },
    },
    {
      label: 'login method',
      initial: { fact: 'avatar', source: 'user_uploaded' },
      kind: 'login_method',
      expected: { fact: 'login_method', method: 'passkey' },
    },
    {
      label: 'avatar',
      initial: { fact: 'login_method', method: 'federation' },
      kind: 'avatar',
      expected: { fact: 'avatar', source: 'any' },
    },
  ]

  it.each(factCases)('switches to the $label leaf without retaining fields from the previous fact', async ({ initial, kind, expected }) => {
    const wrapper = mountEditor(initial)

    await choose(wrapper, 'kind', 'root', kind)

    expect(lastUpdate(wrapper)).toEqual(expected)
  })

  const valueCases: Array<{
    label: string
    initial: Condition
    value: string
    expected: Condition
  }> = [
    {
      label: 'corporate provider',
      initial: { fact: 'connection.provider', provider: 'partners' },
      value: 'corporate',
      expected: { fact: 'connection.provider', provider: 'corporate' },
    },
    {
      label: 'partners provider',
      initial: { fact: 'connection.provider', provider: 'corporate' },
      value: 'partners',
      expected: { fact: 'connection.provider', provider: 'partners' },
    },
    {
      label: 'OIDC protocol',
      initial: { fact: 'connection.protocol', protocol: 'steam' },
      value: 'oidc',
      expected: { fact: 'connection.protocol', protocol: 'oidc' },
    },
    {
      label: 'Steam protocol',
      initial: { fact: 'connection.protocol', protocol: 'oidc' },
      value: 'steam',
      expected: { fact: 'connection.protocol', protocol: 'steam' },
    },
    {
      label: 'VRChat protocol',
      initial: { fact: 'connection.protocol', protocol: 'oidc' },
      value: 'vrchat',
      expected: { fact: 'connection.protocol', protocol: 'vrchat' },
    },
    {
      label: 'passkey login',
      initial: { fact: 'login_method', method: 'federation' },
      value: 'passkey',
      expected: { fact: 'login_method', method: 'passkey' },
    },
    {
      label: 'password plus TOTP login',
      initial: { fact: 'login_method', method: 'passkey' },
      value: 'password_totp',
      expected: { fact: 'login_method', method: 'password_totp' },
    },
    {
      label: 'federated login',
      initial: { fact: 'login_method', method: 'passkey' },
      value: 'federation',
      expected: { fact: 'login_method', method: 'federation' },
    },
    {
      label: 'any avatar source',
      initial: { fact: 'avatar', source: 'user_uploaded' },
      value: 'any',
      expected: { fact: 'avatar', source: 'any' },
    },
    {
      label: 'user-uploaded avatar source',
      initial: { fact: 'avatar', source: 'any' },
      value: 'user_uploaded',
      expected: { fact: 'avatar', source: 'user_uploaded' },
    },
  ]

  it.each(valueCases)('keeps $label selectable instead of collapsing an allowed leaf value', async ({ initial, value, expected }) => {
    const wrapper = mountEditor(initial)

    await choose(wrapper, 'value', 'root', value)

    expect(lastUpdate(wrapper)).toEqual(expected)
  })

  it('derives provider choices only from descriptors instead of exposing arbitrary or stale slugs', async () => {
    const wrapper = mountEditor({ fact: 'connection.provider', provider: 'corporate' })

    await openOptions(wrapper, 'value', 'root')

    const options = Array.from(
      document.body.querySelectorAll<HTMLElement>('[data-test^="condition-option-root-"]'),
    )
    expect(options.map((option) => option.textContent?.trim())).toEqual(['corporate', 'partners'])
  })

  it('keeps not at one singular child instead of accidentally serializing a children array', async () => {
    const wrapper = mountEditor({
      op: 'not',
      child: { fact: 'avatar', source: 'any' },
    })
    const root = wrapper.get('[data-test="condition-node-root"]')

    expect(root.findAll('[data-test^="condition-node-root-"]')).toHaveLength(1)
    expect(wrapper.get('[data-test="condition-node-root-child"]').exists()).toBe(true)
    expect(
      root
        .findAll<HTMLButtonElement>('button[data-test^="add-"]')
        .filter((button) => !button.element.disabled),
    ).toHaveLength(0)

    await choose(wrapper, 'value', 'root-child', 'user_uploaded')

    const update = lastUpdate(wrapper)
    expect(update).toEqual({
      op: 'not',
      child: { fact: 'avatar', source: 'user_uploaded' },
    })
    expect(Object.keys(update).sort()).toEqual(['child', 'op'])
  })

  it.each([
    { label: 'all', modelValue: { op: 'all', children: [] } as Condition },
    { label: 'any', modelValue: { op: 'any', children: [] } as Condition },
  ])('marks an empty $label combinator visibly invalid instead of silently accepting it', ({ modelValue }) => {
    const wrapper = mountEditor(modelValue)
    const node = wrapper.get('[data-test="condition-node-root"]')
    const error = wrapper.get('[data-test="condition-error-root"]')

    expect(node.attributes('aria-invalid')).toBe('true')
    expect(error.attributes('role')).toBe('alert')
    expect(error.text().trim().length).toBeGreaterThan(0)
  })

  it('allows depth eight but disables every add action that would create depth nine', () => {
    const wrapper = mountEditor({
      op: 'all',
      children: [
        {
          op: 'all',
          children: [
            {
              op: 'all',
              children: [
                {
                  op: 'all',
                  children: [
                    {
                      op: 'all',
                      children: [
                        {
                          op: 'all',
                          children: [
                            {
                              op: 'all',
                              children: [{ op: 'all', children: [] }],
                            },
                          ],
                        },
                      ],
                    },
                  ],
                },
              ],
            },
          ],
        },
      ],
    })

    expect(
      wrapper.get<HTMLButtonElement>('[data-test="add-leaf-root-0-0-0-0-0-0"]').element.disabled,
    ).toBe(false)
    expectAllAddControlsDisabled(wrapper, 'root-0-0-0-0-0-0-0')
  })

  it('allows a sixty-fourth node but disables all adds once the tree already has 64 nodes', async () => {
    const sixtyThreeNodes: Condition = {
      op: 'all',
      children: [
        {
          op: 'all',
          children: Array.from(
            { length: 30 },
            (): Condition => ({ fact: 'avatar', source: 'any' }),
          ),
        },
        {
          op: 'all',
          children: Array.from(
            { length: 30 },
            (): Condition => ({ fact: 'avatar', source: 'any' }),
          ),
        },
      ],
    }
    const sixtyFourNodes: Condition = {
      op: 'all',
      children: [
        {
          op: 'all',
          children: Array.from(
            { length: 30 },
            (): Condition => ({ fact: 'avatar', source: 'any' }),
          ),
        },
        {
          op: 'all',
          children: Array.from(
            { length: 31 },
            (): Condition => ({ fact: 'avatar', source: 'any' }),
          ),
        },
      ],
    }
    const wrapper = mountEditor(sixtyThreeNodes)

    expect(wrapper.get<HTMLButtonElement>('[data-test="add-leaf-root"]').element.disabled).toBe(false)
    await wrapper.setProps({ modelValue: sixtyFourNodes })
    expectAllAddControlsDisabled(wrapper, 'root')
  })

  it('allows a thirty-second child but disables all sibling adds when a combinator has 32', async () => {
    const thirtyOneChildren: Condition = {
      op: 'all',
      children: Array.from(
        { length: 31 },
        (): Condition => ({ fact: 'avatar', source: 'any' }),
      ),
    }
    const thirtyTwoChildren: Condition = {
      op: 'all',
      children: Array.from(
        { length: 32 },
        (): Condition => ({ fact: 'avatar', source: 'any' }),
      ),
    }
    const wrapper = mountEditor(thirtyOneChildren)

    expect(wrapper.get<HTMLButtonElement>('[data-test="add-leaf-root"]').element.disabled).toBe(false)
    await wrapper.setProps({ modelValue: thirtyTwoChildren })
    expectAllAddControlsDisabled(wrapper, 'root')
  })

  it('uses labelled groups and focusable native buttons instead of mouse-only node actions', () => {
    const wrapper = mountEditor({
      op: 'all',
      children: [
        { fact: 'avatar', source: 'any' },
        { fact: 'login_method', method: 'passkey' },
      ],
    })
    const nodes = wrapper.findAll('[data-test^="condition-node-"]')

    expect(nodes).toHaveLength(3)
    for (const node of nodes) {
      expect(node.attributes('role')).toBe('group')
      expect(node.attributes('aria-label')?.trim().length).toBeGreaterThan(0)
    }

    for (const selector of ['add-leaf-root', 'remove-root-0', 'remove-root-1']) {
      const button = wrapper.get<HTMLButtonElement>(`[data-test="${selector}"]`)
      expect(button.element.tagName).toBe('BUTTON')
      expect(button.attributes('type')).toBe('button')
      expect(button.attributes('aria-label')?.trim().length).toBeGreaterThan(0)
      expect(button.element.tabIndex).toBeGreaterThanOrEqual(0)
      button.element.focus()
      expect(document.activeElement).toBe(button.element)
    }
  })
})
