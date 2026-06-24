import { describe, it, expect, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import AppTile, { type LaunchpadApp, type ConsentInfo } from './AppTile.vue'
import { DropdownMenu } from '@/components/ui/dropdown-menu'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

const APP: LaunchpadApp = {
  kind: 'oidc',
  id: 'my-app',
  name: 'My App',
  iconUrl: null,
  launchUrl: 'https://app.example.com/',
}
const SAML_APP: LaunchpadApp = { kind: 'saml', id: '42', name: 'Salesforce', iconUrl: null, launchUrl: '/saml/sso/init?sp=x' }
const FWD_APP: LaunchpadApp = { kind: 'forward_auth', id: 'grafana', name: 'Grafana', iconUrl: null, launchUrl: 'https://grafana.example.com/' }

const CONSENT: ConsentInfo = { scopes: ['openid', 'profile'] }
const ACK: ConsentInfo = { scopes: [] } // a SAML acknowledgement carries no scopes

function mountTile(props: Record<string, unknown>) {
  return mount(AppTile, { props: { app: APP, ...props }, global: { plugins: [i18n()] } })
}

// The actions menu is a portaled Reka DropdownMenu — its content (incl. the
// revoke item) only renders when open. Force it open via the root v-model and
// assert against the teleported content on document.body.
async function mountOpen(props: Record<string, unknown>) {
  const w = mount(AppTile, { props, global: { plugins: [i18n()] }, attachTo: document.body })
  w.findComponent(DropdownMenu).vm.$emit('update:open', true)
  await flushPromises()
  return w
}
afterEach(() => { document.body.innerHTML = '' })

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

  it('exposes revoke for a consented OIDC app', async () => {
    await mountOpen({ app: APP, consent: CONSENT })
    expect(document.body.querySelector(`[data-test="revoke-${APP.id}"]`)).not.toBeNull()
  })

  it('exposes revoke for an acknowledged SAML app', async () => {
    await mountOpen({ app: SAML_APP, consent: ACK })
    expect(document.body.querySelector(`[data-test="revoke-${SAML_APP.id}"]`)).not.toBeNull()
  })

  it('emits revoke with the app when the SAML revoke item is selected', async () => {
    const w = await mountOpen({ app: SAML_APP, consent: ACK })
    ;(document.body.querySelector(`[data-test="revoke-${SAML_APP.id}"]`) as HTMLElement).click()
    await flushPromises()
    expect(w.emitted('revoke')?.[0]).toEqual([SAML_APP])
  })

  it('does NOT expose revoke for forward-auth (always-on) or an unconsented app', async () => {
    await mountOpen({ app: FWD_APP, consent: null })
    expect(document.body.querySelector(`[data-test="revoke-${FWD_APP.id}"]`)).toBeNull()
    document.body.innerHTML = ''
    await mountOpen({ app: APP, consent: null })
    expect(document.body.querySelector(`[data-test="revoke-${APP.id}"]`)).toBeNull()
  })
})
