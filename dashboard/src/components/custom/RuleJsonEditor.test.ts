import { afterEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import type { Rule } from '@/lib/appAccess'
import type { InvalidRuleJSON } from '@/lib/ruleDraft'
import RuleJsonEditor from './RuleJsonEditor.vue'

const RULE: Rule = {
  version: 1,
  condition: { fact: 'connection.provider', provider: 'corporate' },
}
const mounted: VueWrapper[] = []
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

function mountEditor(
  source: string,
  error?: InvalidRuleJSON,
) {
  let wrapper: VueWrapper
  wrapper = mount(RuleJsonEditor, {
    props: {
      source,
      parsedRule: RULE,
      error,
      expression: 'provider("corporate")',
      'onUpdate:source': (next: string) => wrapper.setProps({ source: next }),
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

describe('RuleJsonEditor', () => {
  it('renders a non-wrapping native editor with synchronized, hidden line numbers', async () => {
    const wrapper = mountEditor('{\n  "version": 1\n}')
    const textarea = wrapper.get<HTMLTextAreaElement>('[data-test="rule-json-source"]')
    const gutter = wrapper.get<HTMLElement>('[data-test="rule-json-gutter"]')

    expect(textarea.element.tagName).toBe('TEXTAREA')
    expect(textarea.attributes('wrap')).toBe('off')
    expect(textarea.classes()).toContain('font-mono')
    expect(gutter.attributes('aria-hidden')).toBe('true')
    expect(gutter.text()).toBe('1\n2\n3')

    textarea.element.scrollTop = 37
    await textarea.trigger('scroll')
    expect(gutter.element.scrollTop).toBe(37)
  })

  it('inserts two spaces at the current selection when Tab is pressed', async () => {
    const wrapper = mountEditor('{}')
    const textarea = wrapper.get<HTMLTextAreaElement>('[data-test="rule-json-source"]')
    textarea.element.setSelectionRange(1, 1)

    await textarea.trigger('keydown', { key: 'Tab' })

    expect(wrapper.emitted('update:source')).toEqual([['{  }']])
    expect(textarea.element.selectionStart).toBe(3)
    expect(textarea.element.selectionEnd).toBe(3)
  })

  it('lets the next Tab leave the editor after Escape', async () => {
    const wrapper = mountEditor('{}')
    const textarea = wrapper.get<HTMLTextAreaElement>('[data-test="rule-json-source"]')

    await textarea.trigger('keydown', { key: 'Escape' })
    await textarea.trigger('keydown', { key: 'Tab' })

    expect(wrapper.emitted('escape-tab')).toEqual([[]])
    expect(wrapper.emitted('update:source')).toBeUndefined()
    expect(wrapper.get('[data-test="rule-json-help"]').text()).toContain('Escape, then Tab')
  })

  it('exposes format and copy actions without editing the expression', async () => {
    const wrapper = mountEditor('{"version":1}')

    await wrapper.get('[data-test="rule-json-format"]').trigger('click')
    await wrapper.get('[data-test="rule-json-copy"]').trigger('click')
    await wrapper.get('[data-test="rule-expression-copy"]').trigger('click')

    expect(wrapper.emitted('format')).toEqual([[]])
    expect(wrapper.emitted('copy-json')).toEqual([[]])
    expect(wrapper.emitted('copy-expression')).toEqual([[]])
    expect(wrapper.get('[data-test="rule-expression"]').text()).toBe('provider("corporate")')
    expect(wrapper.findAll('textarea')).toHaveLength(1)
    expect(wrapper.find('[data-test="rule-expression"]').attributes('contenteditable')).toBeUndefined()
  })

  it('associates parse errors with the editor using line and column without exposing raw errors', () => {
    const error = {
      ok: false,
      source: '{\n  nope\n}',
      path: '$',
      reason: 'invalid_json',
      line: 2,
      column: 3,
      raw: 'SyntaxError: Unexpected token n at position 4',
    } as InvalidRuleJSON & { raw: string }
    const wrapper = mountEditor(error.source, error)
    const textarea = wrapper.get('[data-test="rule-json-source"]')
    const message = wrapper.get('[data-test="rule-json-error"]')

    expect(message.text()).toContain('Line 2, column 3')
    expect(message.text()).toContain('The rule is not valid JSON.')
    expect(wrapper.text()).not.toContain(error.raw)
    expect(textarea.attributes('aria-invalid')).toBe('true')
    expect(textarea.attributes('aria-describedby')).toContain(message.attributes('id'))
    expect(wrapper.find('[data-test="rule-expression"]').exists()).toBe(false)
  })

  it('identifies semantic errors by their exact JSON path', () => {
    const error: InvalidRuleJSON = {
      ok: false,
      source: '{"version":1}',
      path: '$.condition.children[1]',
      reason: 'provider_not_found',
    }
    const wrapper = mountEditor(error.source, error)

    expect(wrapper.get('[data-test="rule-json-error"]').text()).toBe(
      'At $.condition.children[1]: Choose an available connection provider.',
    )
  })
})
