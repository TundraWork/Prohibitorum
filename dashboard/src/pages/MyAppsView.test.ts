import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))

// The auth store supplies the greeting name + isAdmin; stub it so MyAppsView
// mounts without a Pinia instance.
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ me: { username: 'jesse', displayName: 'Jesse Cheng', role: 'admin' }, isAdmin: true }),
}))

import MyAppsView from './MyAppsView.vue'
import AppTile, { type LaunchpadApp } from '@/components/custom/AppTile.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(MyAppsView, { global: { plugins: [i18n()] }, attachTo: document.body })

// grafana = forward_auth (always connected); linear = oidc consented (connected,
// revocable); nextcloud = oidc not-consented (available); salesforce = saml
// not-acked (available).
const APPS: LaunchpadApp[] = [
  { kind: 'forward_auth', id: 'grafana', name: 'Grafana', iconUrl: null, launchUrl: 'https://grafana.example.com' },
  { kind: 'oidc', id: 'linear', name: 'Linear', iconUrl: null, launchUrl: 'https://linear.example.com' },
  { kind: 'oidc', id: 'nextcloud', name: 'Nextcloud', iconUrl: null, launchUrl: 'https://cloud.example.com' },
  { kind: 'saml', id: '42', name: 'Salesforce', iconUrl: null, launchUrl: '/saml/sso/init?sp=x' },
]
const CONSENTS = [{ kind: 'oidc', clientId: 'linear', scopes: ['openid', 'profile'] }]

// Resolve /me/apps and /me/consent independently from the same mocked get().
function serve(apps: unknown = APPS, consents: unknown = CONSENTS) {
  get.mockImplementation(((path: string) =>
    Promise.resolve(path.includes('/me/consent') ? consents : apps)) as typeof api.get)
}

// ConfirmDialog's confirm button: variant="destructive" carrying the revoke label.
function clickConfirm() {
  const btns = Array.from(document.body.querySelectorAll('button')).filter(
    (b) => b.getAttribute('data-variant') === 'destructive' && b.textContent?.includes(en.myApps.revoke),
  )
  btns[btns.length - 1]!.click()
}

beforeEach(() => { get.mockReset(); post.mockReset() })

describe('MyAppsView', () => {
  it('renders connected apps (consented OIDC + forward-auth) as tiles; available apps are not in the grid', async () => {
    serve()
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain('Grafana')   // forward_auth → always connected
    expect(w.text()).toContain('Linear')    // oidc consented
    expect(w.text()).not.toContain('Nextcloud')   // available → only in the (closed) picker
    expect(w.text()).not.toContain('Salesforce')
  })

  it('Add-app picker lists the authorized-but-not-connected apps', async () => {
    serve()
    const w = mountView(); await flushPromises()
    await w.find('[data-test="add-app"]').trigger('click'); await flushPromises()
    expect(document.body.querySelector('[data-test="connect-oidc-nextcloud"]')).not.toBeNull()
    expect(document.body.querySelector('[data-test="connect-saml-42"]')).not.toBeNull()
    expect(document.body.textContent).toContain('Nextcloud')
    expect(document.body.textContent).toContain('Salesforce')
  })

  it('shows the empty state when the account has no apps', async () => {
    serve([], [])
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.myApps.empty)
  })

  it('surfaces an error when /me/apps fails with an app error', async () => {
    // App 4xx codes still render inline; connectivity/5xx (server_error) are now
    // suppressed here and surfaced via the global toast instead.
    get.mockImplementation(((path: string) =>
      path.includes('/me/consent')
        ? Promise.resolve([])
        : Promise.reject({ code: 'forbidden', message: 'boom' })) as typeof api.get)
    const w = mountView(); await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(true)
  })

  it('does NOT render an inline alert for server_error (global toast owns it)', async () => {
    get.mockImplementation(((path: string) =>
      path.includes('/me/consent')
        ? Promise.resolve([])
        : Promise.reject({ code: 'server_error', message: 'boom' })) as typeof api.get)
    const w = mountView(); await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(true)
  })

  it('surfaces an error when /me/consent fails (no misleading connected/available split)', async () => {
    get.mockImplementation(((path: string) =>
      path.includes('/me/consent')
        ? Promise.reject({ code: 'forbidden', message: 'boom' })
        : Promise.resolve(APPS)) as typeof api.get)
    const w = mountView(); await flushPromises()
    // The whole load fails atomically: alert shown, and NO app grid rendered
    // (which would otherwise wrongly show every app as "available").
    expect(w.find('[role="alert"]').exists()).toBe(true)
    expect(w.findComponent(AppTile).exists()).toBe(false)
  })

  it('revokes a connected OIDC app with an explicit kind, then refreshes', async () => {
    serve()
    post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    const getCallsBefore = get.mock.calls.length

    const tile = w.findAllComponents(AppTile).find((t) => (t.props('app') as LaunchpadApp).id === 'linear')!
    tile.vm.$emit('revoke', tile.props('app'))
    await flushPromises()
    clickConfirm(); await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/consent/revoke', { kind: 'oidc', clientId: 'linear' })
    expect(get.mock.calls.length).toBeGreaterThan(getCallsBefore) // refreshed
  })

  it('surfaces an acknowledged SAML app as a revocable grant and revokes it (posts kind=saml)', async () => {
    serve(APPS, [
      { kind: 'oidc', clientId: 'linear', scopes: ['openid'] },
      { kind: 'saml', clientId: '42', scopes: [] }, // Salesforce now acknowledged → connected
    ])
    post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()

    const tile = w.findAllComponents(AppTile).find((t) => (t.props('app') as LaunchpadApp).id === '42')!
    expect(tile.props('consent')).not.toBeNull() // passed a grant marker → tile shows connected + revoke
    tile.vm.$emit('revoke', tile.props('app'))
    await flushPromises()
    clickConfirm(); await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/consent/revoke', { kind: 'saml', clientId: '42' })
  })

  it('search filters connected apps by name and shows a no-match message', async () => {
    serve()
    const w = mountView(); await flushPromises()

    await w.find('input').setValue('linear'); await flushPromises()
    expect(w.text()).toContain('Linear')
    expect(w.text()).not.toContain('Grafana')
    expect(w.find('[data-test="add-app"]').exists()).toBe(false) // hidden while searching

    await w.find('input').setValue('zzzz'); await flushPromises()
    expect(w.text()).toContain('No apps match')
  })
})
