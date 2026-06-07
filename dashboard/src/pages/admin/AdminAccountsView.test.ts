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
  it('shows empty state', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.accounts.empty)
  })
  it('surfaces error', async () => {
    get.mockRejectedValue({ code: 'server_error', message: 'boom' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.errors.server_error)
  })
})
