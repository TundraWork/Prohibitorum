import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import en from '../locales/en'
import ConsentScopeList from './ConsentScopeList.vue'

// The Nuxt UI Vite plugin (registered in vitest.config.ts) resolves <UIcon>,
// so this real mount works without stubs.
function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}

function mountList(scopes: string[]) {
  return mount(ConsentScopeList, {
    props: { scopes },
    global: { plugins: [makeI18n()] },
  })
}

describe('ConsentScopeList', () => {
  it('renders localized labels for known scopes and raw names for unknown ones', () => {
    const wrapper = mountList(['openid', 'profile', 'x:custom'])

    const text = wrapper.text()
    // Known scopes use the en label.
    expect(text).toContain(en.scopes.openid)
    expect(text).toContain(en.scopes.profile)
    // Unknown scope falls back to the raw name.
    expect(text).toContain('x:custom')
  })

  it('renders one list item per scope', () => {
    const wrapper = mountList(['openid', 'email', 'offline_access'])
    expect(wrapper.findAll('li').length).toBe(3)
  })
})
