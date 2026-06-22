import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import AppTile, { type LaunchpadApp, type ConsentInfo } from './AppTile.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

const APP: LaunchpadApp = {
  kind: 'oidc',
  id: 'my-app',
  name: 'My App',
  iconUrl: null,
  launchUrl: 'https://app.example.com/',
}

const CONSENT: ConsentInfo = { scopes: ['openid', 'profile'] }

describe('AppTile', () => {
  it('launch anchor has correct href, target, and rel', () => {
    const w = mount(AppTile, {
      props: { app: APP },
      global: { plugins: [i18n()] },
    })
    const anchor = w.find(`[data-test="launch-${APP.id}"]`)
    expect(anchor.exists()).toBe(true)
    expect(anchor.attributes('href')).toBe(APP.launchUrl)
    expect(anchor.attributes('target')).toBe('_blank')
    expect(anchor.attributes('rel')).toContain('noopener')
  })

  it('consent glyph is ABSENT when consent is not provided', () => {
    const w = mount(AppTile, {
      props: { app: APP },
      global: { plugins: [i18n()] },
    })
    expect(w.find(`[data-test="consent-${APP.id}"]`).exists()).toBe(false)
  })

  it('consent glyph is PRESENT when consent is provided', () => {
    const w = mount(AppTile, {
      props: { app: APP, consent: CONSENT },
      global: { plugins: [i18n()] },
    })
    expect(w.find(`[data-test="consent-${APP.id}"]`).exists()).toBe(true)
  })

  it('consent glyph is ABSENT when consent is null', () => {
    const w = mount(AppTile, {
      props: { app: APP, consent: null },
      global: { plugins: [i18n()] },
    })
    expect(w.find(`[data-test="consent-${APP.id}"]`).exists()).toBe(false)
  })

  it('kebab menu is ABSENT when consent is not provided', () => {
    const w = mount(AppTile, {
      props: { app: APP },
      global: { plugins: [i18n()] },
    })
    expect(w.find(`[data-test="menu-${APP.id}"]`).exists()).toBe(false)
  })

  it('kebab menu is PRESENT when consent is provided', () => {
    const w = mount(AppTile, {
      props: { app: APP, consent: CONSENT },
      global: { plugins: [i18n()] },
    })
    expect(w.find(`[data-test="menu-${APP.id}"]`).exists()).toBe(true)
  })
})
