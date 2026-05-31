import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import en from '../locales/en'
import CredentialsView from './CredentialsView.vue'

const get = vi.fn()
const post = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a) } }))

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}
beforeEach(() => { get.mockReset(); post.mockReset() })

const creds = [
  { id: 1, credentialIdSuffix: 'ab12', nickname: 'Laptop', transports: ['internal'], backupState: false, attestationType: 'none', createdAt: '2026-01-01T00:00:00Z' },
  { id: 2, credentialIdSuffix: 'cd34', nickname: null, transports: ['usb'], backupState: false, attestationType: 'none', createdAt: '2026-01-01T00:00:00Z' },
]

describe('CredentialsView', () => {
  it('lists credentials', async () => {
    get.mockResolvedValueOnce(creds)
    const wrapper = mount(CredentialsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    expect(wrapper.findAll('tbody tr').length).toBe(2)
    expect(wrapper.text()).toContain('Laptop')
  })

  it('surfaces a delete rejection (last passkey)', async () => {
    get.mockResolvedValueOnce(creds)
    post.mockRejectedValueOnce({ code: 'last_passkey', message: 'cannot remove last passkey' })
    const wrapper = mount(CredentialsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    await wrapper.findAll('[data-test="del"]')[0].trigger('click') // arm row 1
    await wrapper.findAll('[data-test="del"]')[0].trigger('click') // confirm row 1
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/credentials/delete', { id: 1 })
    expect(wrapper.find('[role="alert"]').text()).toContain('cannot remove last passkey')
  })
})
