import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import en from '../locales/en'
import AccountsView from './AccountsView.vue'

const get = vi.fn()
const post = vi.fn()
const put = vi.fn()
vi.mock('../lib/api', () => ({ api: {
  get: (...a: unknown[]) => get(...a),
  post: (...a: unknown[]) => post(...a),
  put: (...a: unknown[]) => put(...a),
} }))

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}
beforeEach(() => { get.mockReset(); post.mockReset(); put.mockReset() })

const accts = [
  { id: 1, username: 'admin', displayName: 'Admin', role: 'admin', disabled: false, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' },
  { id: 2, username: 'bob', displayName: 'Bob', role: 'user', disabled: false, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' },
]

describe('AccountsView', () => {
  it('renders accounts', async () => {
    get.mockResolvedValueOnce(accts)
    const wrapper = mount(AccountsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    expect(wrapper.findAll('tbody tr').length).toBe(2)
  })

  it('disable sends PUT with current displayName+role and flipped disabled', async () => {
    get.mockResolvedValueOnce(accts)
    put.mockResolvedValueOnce({ ...accts[1], disabled: true })
    get.mockResolvedValueOnce([accts[0], { ...accts[1], disabled: true }])
    const wrapper = mount(AccountsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    await wrapper.findAll('[data-test="toggle"]')[1].trigger('click')
    await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/accounts/2', { displayName: 'Bob', role: 'user', disabled: true })
  })

  it('reissue shows the returned url', async () => {
    get.mockResolvedValueOnce(accts)
    post.mockResolvedValueOnce({ url: 'http://x/enroll/tok', expiresAt: '2026-02-01T00:00:00Z' })
    const wrapper = mount(AccountsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    await wrapper.findAll('[data-test="reissue"]')[1].trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/accounts/reissue-enrollment', { id: 2 })
    expect(wrapper.text()).toContain('http://x/enroll/tok')
  })
})
