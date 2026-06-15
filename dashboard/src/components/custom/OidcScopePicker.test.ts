import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import OidcScopePicker from './OidcScopePicker.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

const mountPicker = (modelValue: string[]) =>
  mount(OidcScopePicker, {
    props: { modelValue },
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })

describe('OidcScopePicker', () => {
  it('renders all 4 scope rows', () => {
    const w = mountPicker(['openid'])
    expect(w.find('[data-test="scope-row-openid"]').exists()).toBe(true)
    expect(w.find('[data-test="scope-row-profile"]').exists()).toBe(true)
    expect(w.find('[data-test="scope-row-email"]').exists()).toBe(true)
    expect(w.find('[data-test="scope-row-offline_access"]').exists()).toBe(true)
  })

  it('openid is checked and disabled (required)', () => {
    const w = mountPicker(['openid'])
    const cb = w.find('[data-test="scope-checkbox-openid"]')
    expect(cb.attributes('data-state')).toBe('checked')
    expect(cb.attributes('disabled')).toBeDefined()
  })

  it('profile/email/offline_access are not disabled', () => {
    const w = mountPicker(['openid'])
    for (const scope of ['profile', 'email', 'offline_access']) {
      const cb = w.find(`[data-test="scope-checkbox-${scope}"]`)
      expect(cb.attributes('disabled')).toBeUndefined()
    }
  })

  it('reflects checked state for scopes in modelValue', () => {
    const w = mountPicker(['openid', 'email'])
    expect(w.find('[data-test="scope-checkbox-email"]').attributes('data-state')).toBe('checked')
    expect(w.find('[data-test="scope-checkbox-profile"]').attributes('data-state')).toBe('unchecked')
  })

  it('toggling an unchecked scope emits the updated array with it added', async () => {
    const w = mountPicker(['openid'])
    await w.find('[data-test="scope-checkbox-profile"]').trigger('click')
    const emitted = w.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    expect((emitted![0][0] as string[])).toContain('openid')
    expect((emitted![0][0] as string[])).toContain('profile')
  })

  it('toggling a checked scope emits the updated array with it removed', async () => {
    const w = mountPicker(['openid', 'email'])
    await w.find('[data-test="scope-checkbox-email"]').trigger('click')
    const emitted = w.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    expect((emitted![0][0] as string[])).not.toContain('email')
    expect((emitted![0][0] as string[])).toContain('openid')
  })

  it('descriptions render for each scope', () => {
    const w = mountPicker(['openid'])
    expect(w.find('[data-test="scope-desc-openid"]').text()).toBe(en.admin.upstream.scopeSuggestions.openid)
    expect(w.find('[data-test="scope-desc-profile"]').text()).toBe(en.admin.upstream.scopeSuggestions.profile)
    expect(w.find('[data-test="scope-desc-email"]').text()).toBe(en.admin.upstream.scopeSuggestions.email)
    expect(w.find('[data-test="scope-desc-offline_access"]').text()).toBe(en.admin.upstream.scopeSuggestions.offline_access)
  })
})
