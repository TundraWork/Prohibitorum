import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

import AppAccessView from './AppAccessView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AppAccessView, { global: { plugins: [i18n()] }, attachTo: document.body })

const APPS = [
  { clientId: 'grafana', name: 'Grafana', scopes: ['openid'], grantedAt: '2026-01-01T00:00:00Z' },
  { clientId: 'argocd', name: 'Argo CD', iconUrl: '/icon/entity/argocd', scopes: ['openid', 'profile'], grantedAt: '2026-02-01T00:00:00Z' },
]

// ConfirmDialog confirm button: variant="destructive" carrying the revoke label.
function clickConfirm() {
  const confirmBtns = Array.from(document.body.querySelectorAll('button'))
    .filter((b) => b.getAttribute('data-variant') === 'destructive'
      && b.textContent?.includes(en.appAccess.revoke))
  confirmBtns[confirmBtns.length - 1]!.click()
}

beforeEach(() => { get.mockReset(); post.mockReset() })

describe('AppAccessView', () => {
  it('lists consented apps with name and scopes', async () => {
    get.mockResolvedValue(APPS)
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain('Grafana')
    expect(w.text()).toContain('openid')
    expect(w.text()).toContain('Argo CD')
    expect(w.text()).toContain('profile')
  })

  it('shows empty state when no apps have been consented', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.appAccess.empty)
  })

  it('revokes an app (confirm → post → refresh)', async () => {
    get.mockResolvedValue(APPS)
    post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="revoke-grafana"]').trigger('click'); await flushPromises()
    clickConfirm(); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/consent/revoke', { clientId: 'grafana' })
    // refresh: get called a second time
    expect(get.mock.calls.filter((c) => String(c[0]).includes('/me/consent'))).toHaveLength(2)
  })

  it('shows revoke confirm dialog with app name when revoke button is clicked', async () => {
    get.mockResolvedValue(APPS)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="revoke-grafana"]').trigger('click'); await flushPromises()
    // Dialog should be open — confirm button should be present
    const confirmBtns = Array.from(document.body.querySelectorAll('button'))
      .filter((b) => b.getAttribute('data-variant') === 'destructive'
        && b.textContent?.includes(en.appAccess.revoke))
    expect(confirmBtns.length).toBeGreaterThan(0)
  })

  it('does not call post when confirm dialog is cancelled', async () => {
    get.mockResolvedValue(APPS)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="revoke-grafana"]').trigger('click'); await flushPromises()
    // Cancel via @cancel event — find cancel button
    const cancelBtns = Array.from(document.body.querySelectorAll('button'))
      .filter((b) => b.textContent?.trim() === en.common.cancel)
    if (cancelBtns.length > 0) {
      cancelBtns[cancelBtns.length - 1]!.click()
      await flushPromises()
    }
    expect(post).not.toHaveBeenCalled()
  })

  it('shows error alert when get fails', async () => {
    get.mockRejectedValue({ code: 'server_error', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(true)
  })

  it('app with iconUrl renders an <img> with that src', async () => {
    get.mockResolvedValue(APPS)
    const w = mountView(); await flushPromises()
    // Find the img inside the argocd card by locating it near the revoke button
    const imgs = w.findAll('img')
    const argoImg = imgs.find((i) => i.attributes('src')?.includes('/icon/entity/argocd'))
    expect(argoImg).toBeDefined()
    expect(argoImg!.attributes('src')).toContain('/icon/entity/argocd')
  })
})
