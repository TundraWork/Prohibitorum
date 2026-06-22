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

function mountTile(props: Record<string, unknown>) {
  return mount(AppTile, { props: { app: APP, ...props }, global: { plugins: [i18n()] } })
}

describe('AppTile', () => {
  it('launch overlay has correct href, target, and rel', () => {
    const w = mountTile({})
    const anchor = w.find(`[data-test="launch-${APP.id}"]`)
    expect(anchor.exists()).toBe(true)
    expect(anchor.attributes('href')).toBe(APP.launchUrl)
    expect(anchor.attributes('target')).toBe('_blank')
    expect(anchor.attributes('rel')).toContain('noopener')
  })

  it('renders the protocol badge for the app kind', () => {
    const w = mountTile({})
    expect(w.find(`[data-test="protocol-${APP.kind}"]`).exists()).toBe(true)
  })

  it('access-granted affordance follows consent', () => {
    expect(mountTile({}).find(`[data-test="consent-${APP.id}"]`).exists()).toBe(false)
    expect(mountTile({ consent: null }).find(`[data-test="consent-${APP.id}"]`).exists()).toBe(false)
    expect(mountTile({ consent: CONSENT }).find(`[data-test="consent-${APP.id}"]`).exists()).toBe(true)
  })

  it('actions menu trigger is ALWAYS present (every tile is manageable)', () => {
    expect(mountTile({}).find(`[data-test="menu-${APP.id}"]`).exists()).toBe(true)
    expect(mountTile({ consent: CONSENT }).find(`[data-test="menu-${APP.id}"]`).exists()).toBe(true)
    expect(mountTile({ isAdmin: true }).find(`[data-test="menu-${APP.id}"]`).exists()).toBe(true)
  })
})
