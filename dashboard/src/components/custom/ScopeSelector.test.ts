import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import ScopeSelector from './ScopeSelector.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

const OIDC_KNOWN = [
  { value: 'openid',         description: 'Required scope.', required: true },
  { value: 'profile',        description: 'Basic profile info.' },
  { value: 'email',          description: 'Email address.' },
  { value: 'offline_access', description: 'Refresh tokens.' },
]

const UPSTREAM_KNOWN = [
  { value: 'openid',  description: 'Required scope.' },
  { value: 'profile', description: 'Basic profile info.' },
  { value: 'email',   description: 'Email address.' },
]

const mount_ = (props: Record<string, unknown>) =>
  mount(ScopeSelector, {
    props: { known: OIDC_KNOWN, allowCustom: false, modelValue: ['openid'], ...props },
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })

describe('ScopeSelector — known scopes (OIDC mode)', () => {
  it('renders all known scope rows', () => {
    const w = mount_({ known: OIDC_KNOWN, modelValue: ['openid'] })
    for (const s of OIDC_KNOWN) {
      expect(w.find(`[data-test="scope-row-${s.value}"]`).exists()).toBe(true)
    }
  })

  it('required scope (openid) is checked and disabled', () => {
    const w = mount_({ known: OIDC_KNOWN, modelValue: ['openid'] })
    const cb = w.find('[data-test="scope-checkbox-openid"]')
    expect(cb.attributes('data-state')).toBe('checked')
    expect(cb.attributes('disabled')).toBeDefined()
  })

  it('required scope is always present in emitted value even when not in initial modelValue', async () => {
    // Start without openid to verify the toggle still forces it into the emit.
    const w = mount_({ known: OIDC_KNOWN, modelValue: [] })
    // toggling a non-required scope to on should still include openid
    await w.find('[data-test="scope-checkbox-profile"]').trigger('click')
    const emitted = w.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    const val = emitted![0][0] as string[]
    expect(val).toContain('openid')
    expect(val).toContain('profile')
  })

  it('non-required scopes are not disabled', () => {
    const w = mount_({ known: OIDC_KNOWN, modelValue: ['openid'] })
    for (const s of ['profile', 'email', 'offline_access']) {
      expect(w.find(`[data-test="scope-checkbox-${s}"]`).attributes('disabled')).toBeUndefined()
    }
  })

  it('reflects checked state for scopes in modelValue', () => {
    const w = mount_({ known: OIDC_KNOWN, modelValue: ['openid', 'email'] })
    expect(w.find('[data-test="scope-checkbox-email"]').attributes('data-state')).toBe('checked')
    expect(w.find('[data-test="scope-checkbox-profile"]').attributes('data-state')).toBe('unchecked')
  })

  it('toggling an unchecked known scope emits array with it added (openid preserved)', async () => {
    const w = mount_({ known: OIDC_KNOWN, modelValue: ['openid'] })
    await w.find('[data-test="scope-checkbox-profile"]').trigger('click')
    const emitted = w.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    const val = emitted![0][0] as string[]
    expect(val).toContain('openid')
    expect(val).toContain('profile')
  })

  it('toggling a checked non-required scope emits array with it removed', async () => {
    const w = mount_({ known: OIDC_KNOWN, modelValue: ['openid', 'email'] })
    await w.find('[data-test="scope-checkbox-email"]').trigger('click')
    const emitted = w.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    const val = emitted![0][0] as string[]
    expect(val).not.toContain('email')
    expect(val).toContain('openid')
  })

  it('renders descriptions for each scope row', () => {
    const w = mount_({ known: OIDC_KNOWN, modelValue: ['openid'] })
    expect(w.find('[data-test="scope-desc-openid"]').text()).toBe('Required scope.')
    expect(w.find('[data-test="scope-desc-profile"]').text()).toBe('Basic profile info.')
  })

  it('does NOT render the custom scopes section when allowCustom is false', () => {
    const w = mount_({ known: OIDC_KNOWN, allowCustom: false, modelValue: ['openid'] })
    expect(w.find('[data-test="custom-scope-input"]').exists()).toBe(false)
    expect(w.find('[data-test="custom-scopes-label"]').exists()).toBe(false)
  })
})

describe('ScopeSelector — custom scopes (upstream mode)', () => {
  const mountUpstream = (modelValue: string[]) =>
    mount(ScopeSelector, {
      props: { known: UPSTREAM_KNOWN, allowCustom: true, modelValue },
      global: { plugins: [i18n()] },
      attachTo: document.body,
    })

  it('shows the custom-scopes section when allowCustom is true', () => {
    const w = mountUpstream(['openid'])
    expect(w.find('[data-test="custom-scope-input"]').exists()).toBe(true)
    expect(w.find('[data-test="custom-scopes-label"]').exists()).toBe(true)
  })

  it('adding a custom scope via Enter emits the new value appended', async () => {
    const w = mountUpstream(['openid'])
    const input = w.find('[data-test="custom-scope-input"]')
    await input.setValue('custom_scope')
    await input.trigger('keydown', { key: 'Enter' })
    const emitted = w.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    const val = emitted![0][0] as string[]
    expect(val).toContain('openid')
    expect(val).toContain('custom_scope')
  })

  it('adding a custom scope via Add button emits the new value', async () => {
    const w = mountUpstream(['openid'])
    await w.find('[data-test="custom-scope-input"]').setValue('extra_scope')
    await w.find('[data-test="custom-scope-add"]').trigger('click')
    const emitted = w.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    const val = emitted![0][0] as string[]
    expect(val).toContain('extra_scope')
  })

  it('adding a custom scope via comma key emits the new value', async () => {
    const w = mountUpstream(['openid'])
    const input = w.find('[data-test="custom-scope-input"]')
    await input.setValue('another_scope')
    await input.trigger('keydown', { key: ',' })
    const emitted = w.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    const val = emitted![0][0] as string[]
    expect(val).toContain('another_scope')
  })

  it('renders chips for custom scopes already in modelValue', () => {
    const w = mountUpstream(['openid', 'my_custom'])
    expect(w.find('[data-test="custom-chip-my_custom"]').exists()).toBe(true)
  })

  it('removing a custom chip emits array without it', async () => {
    const w = mountUpstream(['openid', 'my_custom', 'other'])
    await w.find('[data-test="custom-chip-remove-my_custom"]').trigger('click')
    const emitted = w.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    const val = emitted![0][0] as string[]
    expect(val).not.toContain('my_custom')
    expect(val).toContain('openid')
    expect(val).toContain('other')
  })

  it('does not add a duplicate custom scope', async () => {
    const w = mountUpstream(['openid', 'my_custom'])
    const input = w.find('[data-test="custom-scope-input"]')
    await input.setValue('my_custom')
    await input.trigger('keydown', { key: 'Enter' })
    expect(w.emitted('update:modelValue')).toBeUndefined()
  })

  it('known scopes appear before custom in the emitted array', async () => {
    // Start with a custom scope already present, then add openid via checkbox.
    // The emitted value should have openid (a known scope) before zzz_custom (a custom scope).
    const w = mountUpstream(['zzz_custom'])
    await w.find('[data-test="scope-checkbox-openid"]').trigger('click')
    const emitted = w.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    const val = emitted![0][0] as string[]
    expect(val).toContain('openid')
    expect(val).toContain('zzz_custom')
    expect(val.indexOf('openid')).toBeLessThan(val.indexOf('zzz_custom'))
  })

  it('Backspace with empty draft removes the last custom scope chip', async () => {
    const w = mountUpstream(['openid', 'c1', 'c2'])
    const input = w.find('[data-test="custom-scope-input"]')
    await input.trigger('keydown', { key: 'Backspace' })
    const emitted = w.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    const val = emitted![0][0] as string[]
    expect(val).not.toContain('c2')
    expect(val).toContain('c1')
  })
})
