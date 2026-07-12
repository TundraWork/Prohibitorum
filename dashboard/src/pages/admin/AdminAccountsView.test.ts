import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
import AdminAccountsView from './AdminAccountsView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminAccountsView, { global: { plugins: [i18n()] } })
const ACCOUNTS = [
  { id: 1, username: 'alice', displayName: 'Alice Smith', role: 'admin', disabled: false, lastSignInAt: '2026-06-08T00:00:00Z' },
  { id: 2, username: 'bob', displayName: 'Bob Lee', role: 'user', disabled: true },
]
beforeEach(() => { get.mockReset(); push.mockReset() })

describe('AdminAccountsView', () => {
  it('lists accounts with role and state', async () => {
    get.mockResolvedValue(ACCOUNTS)
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/accounts')
    expect(w.text()).toContain('Alice Smith')
    expect(w.text()).toContain('@bob')
    expect(w.text()).toContain(en.admin.accounts.disabled)
  })
  it('row click navigates to the detail page', async () => {
    get.mockResolvedValue(ACCOUNTS)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="account-row-2"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/accounts/2')
  })
  it('row is keyboard-activatable (Enter)', async () => {
    get.mockResolvedValue(ACCOUNTS)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="account-row-2"]').trigger('keydown.enter')
    expect(push).toHaveBeenCalledWith('/admin/accounts/2')
  })
  it('invite navigates to invitations', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    await w.find('[data-test="invite"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/invitations')
  })
  it('shows empty state with no CTA button inside it', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.accounts.empty)
    // The empty state no longer has a duplicate CTA button inside it
    expect(w.find('[data-test="invite"]').exists()).toBe(true) // top button still exists
  })
  it('filter hides non-matching rows', async () => {
    get.mockResolvedValue(ACCOUNTS)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="accounts-filter"]').setValue('alice')
    expect(w.find('[data-test="account-row-1"]').exists()).toBe(true)
    expect(w.find('[data-test="account-row-2"]').exists()).toBe(false)
  })
  it('filter is case-insensitive and matches displayName', async () => {
    get.mockResolvedValue(ACCOUNTS)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="accounts-filter"]').setValue('BOB')
    expect(w.find('[data-test="account-row-2"]').exists()).toBe(true)
    expect(w.find('[data-test="account-row-1"]').exists()).toBe(false)
  })
  it('shows no-matches message when filter excludes everything', async () => {
    get.mockResolvedValue(ACCOUNTS)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="accounts-filter"]').setValue('zzz-no-match')
    expect(w.find('[data-test="accounts-no-matches"]').exists()).toBe(true)
    expect(w.find('[data-test="account-row-1"]').exists()).toBe(false)
  })
  it('surfaces an app load error inline', async () => {
    // App 4xx codes still render inline; connectivity/5xx (server_error) are now
    // suppressed here and surfaced via the global toast instead.
    get.mockRejectedValue({ code: 'forbidden', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.errors.codes.forbidden)
  })

  it('does NOT render server_error inline (global toast owns it)', async () => {
    get.mockRejectedValue({ code: 'server_error', message: 'boom' })
    const w = mountView(); await flushPromises()
    expect(w.text()).not.toContain(en.errors.codes.server_error)
  })
  it('shows the admin diagnostic action when an error with requestId occurs', async () => {
    get.mockRejectedValue({ code: 'forbidden', requestId: 'rid-adm-1' })
    const w = mountView(); await flushPromises()
    expect(w.find('[data-test="error-diagnostic"]').exists()).toBe(true)
  })
})
