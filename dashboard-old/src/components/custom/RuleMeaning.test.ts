import { afterEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import zh from '@/locales/zh'
import type { Condition, ProviderDescriptor, Rule } from '@/lib/appAccess'
import { validateRule } from '@/lib/ruleDraft'
import RuleMeaning from './RuleMeaning.vue'

const PROVIDERS: ProviderDescriptor[] = [
  { slug: 'corporate', displayName: 'Corporate identity' },
]
const mounted: VueWrapper[] = []

function mountMeaning(rule: Rule, options: { compact?: boolean; locale?: 'en' | 'zh' } = {}) {
  const locale = options.locale ?? 'en'
  const wrapper = mount(RuleMeaning, {
    props: { rule, providers: PROVIDERS, compact: options.compact },
    global: {
      plugins: [createI18n({
        legacy: false,
        locale,
        fallbackLocale: 'en',
        messages: { en, zh },
      })],
    },
  })
  mounted.push(wrapper)
  return wrapper
}

afterEach(() => {
  while (mounted.length) mounted.pop()!.unmount()
})

describe('RuleMeaning', () => {
  it('renders positive and negative leaves as prose and emphasizes their values', () => {
    const positive = mountMeaning({
      version: 1,
      condition: { fact: 'connection.provider', provider: 'corporate' },
    })
    const negative = mountMeaning({
      version: 1,
      condition: { op: 'not', child: { fact: 'login_method', method: 'password_totp' } },
    })

    expect(positive.get('[data-test="rule-meaning-first-line"]').text()).toBe(
      'Connection provider is Corporate identity',
    )
    expect(positive.get('strong').text()).toBe('Corporate identity')
    expect(negative.get('[data-test="rule-meaning-first-line"]').text()).toBe(
      'Login method is not Password and TOTP',
    )
    expect(negative.get('strong').text()).toBe('Password and TOTP')
  })

  it('falls back to the exact provider slug when its descriptor is unavailable', () => {
    const wrapper = mountMeaning({
      version: 1,
      condition: { fact: 'connection.provider', provider: 'former-provider' },
    })

    expect(wrapper.text()).toContain('Connection provider is former-provider')
    expect(wrapper.get('strong').text()).toBe('former-provider')
  })

  it.each([
    { label: 'provider', leaf: { fact: 'connection.provider' } as Condition, subject: 'Connection provider' },
    { label: 'protocol', leaf: { fact: 'connection.protocol' } as Condition, subject: 'Connection protocol' },
    { label: 'login method', leaf: { fact: 'login_method' } as Condition, subject: 'Login method' },
    { label: 'avatar', leaf: { fact: 'avatar' } as Condition, subject: 'Avatar' },
  ])('renders localized incomplete values for positive and negative $label leaves', ({ leaf, subject }) => {
    const positive = mountMeaning({ version: 1, condition: leaf })
    const negative = mountMeaning({
      version: 1,
      condition: { op: 'not', child: leaf },
    })

    expect(positive.get('[data-test="rule-meaning-first-line"]').text()).toBe(
      `${subject} is value not selected`,
    )
    expect(negative.get('[data-test="rule-meaning-first-line"]').text()).toBe(
      `${subject} is not value not selected`,
    )
  })

  it('localizes an incomplete leaf value in Chinese', () => {
    const wrapper = mountMeaning({
      version: 1,
      condition: { fact: 'avatar' },
    }, { locale: 'zh' })

    expect(wrapper.get('[data-test="rule-meaning-first-line"]').text()).toBe('头像是尚未选择值')
  })

  it('renders ALL and ANY scopes as a semantic nested sentence outline', () => {
    const wrapper = mountMeaning({
      version: 1,
      condition: {
        op: 'all',
        children: [
          { fact: 'connection.provider', provider: 'corporate' },
          {
            op: 'any',
            children: [
              { fact: 'login_method', method: 'federation' },
              { fact: 'login_method', method: 'passkey' },
            ],
          },
          { op: 'not', child: { fact: 'login_method', method: 'password_totp' } },
        ],
      },
    })

    expect(wrapper.get('[data-test="rule-meaning-first-line"]').text()).toBe('Every condition is true:')
    expect(wrapper.findAll('ul')).toHaveLength(2)
    expect(wrapper.findAll('li')).toHaveLength(5)
    expect(wrapper.text()).toContain('At least one condition is true:')
    expect(wrapper.text()).toContain('Login method is not Password and TOTP')
    expect(wrapper.text()).not.toContain('Match an account when all of the following are true')
  })

  it('summarizes the first line, leaf count, and nested-group count in compact mode', () => {
    const wrapper = mountMeaning({
      version: 1,
      condition: {
        op: 'all',
        children: [
          { fact: 'avatar', source: 'any' },
          { op: 'any', children: [
            { fact: 'login_method', method: 'passkey' },
            { fact: 'login_method', method: 'federation' },
          ] },
        ],
      },
    }, { compact: true })

    expect(wrapper.get('[data-test="rule-meaning-first-line"]').text()).toBe('Every condition is true')
    expect(wrapper.get('[data-test="rule-meaning-counts"]').text()).toBe('3 conditions · 1 nested group')
    expect(wrapper.find('ul').exists()).toBe(false)
  })

  it('localizes the outline grammar instead of exposing English helper text', () => {
    const wrapper = mountMeaning({
      version: 1,
      condition: {
        op: 'any',
        children: [
          { fact: 'avatar', source: 'user_uploaded' },
          { op: 'not', child: { fact: 'connection.protocol', protocol: 'oidc' } },
        ],
      },
    }, { locale: 'zh' })

    expect(wrapper.get('[data-test="rule-meaning-first-line"]').text()).toBe('至少一个条件成立：')
    expect(wrapper.text()).toContain('头像是用户上传')
    expect(wrapper.text()).toContain('连接协议不是OIDC')
    expect(wrapper.text()).not.toContain('At least one condition')
  })

  it('relies on the shared validator to reject root and nested group NOT', () => {
    const rootNot: Rule = {
      version: 1,
      condition: { op: 'not', child: { op: 'all', children: [{ fact: 'avatar', source: 'any' }] } },
    }
    const nestedNot: Rule = {
      version: 1,
      condition: {
        op: 'all',
        children: [
          { op: 'not', child: { op: 'any', children: [{ fact: 'avatar', source: 'any' }] } },
        ],
      },
    }
    const providers = new Set(PROVIDERS.map(({ slug }) => slug))

    expect(validateRule(rootNot, providers)).toMatchObject([
      { path: '$.condition.child', reason: 'not_requires_fact' },
    ])
    expect(validateRule(nestedNot, providers)).toMatchObject([
      { path: '$.condition.children[0].child', reason: 'not_requires_fact' },
    ])
  })
})
