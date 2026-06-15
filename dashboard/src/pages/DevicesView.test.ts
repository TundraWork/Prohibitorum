import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

import DevicesView from './DevicesView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(DevicesView, { global: { plugins: [i18n()] }, attachTo: document.body })

const LOOKUP = {
  pairingId: 'p1', displayCode: 'AB12-CD34',
  initiatorUa: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36',
  initiatorIp: '10.0.0.9',
  createdAt: '2026-01-01T00:00:00Z', expiresAt: '2026-01-01T00:10:00Z', alreadyBound: false,
}
beforeEach(() => { get.mockReset(); post.mockReset() })

async function enterCodeAndLookup(w: ReturnType<typeof mountView>, code = 'AB12-CD34') {
  await w.find('input[name="code"]').setValue(code)
  await w.find('[data-test="lookup"]').trigger('click')
  await flushPromises()
}

describe('DevicesView', () => {
  it('looks up a code and shows the confirmation card', async () => {
    get.mockResolvedValue(LOOKUP)
    const w = mountView()
    await enterCodeAndLookup(w)
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/me/devices/pair/lookup?code=AB12-CD34')
    expect(w.text()).toContain('Chrome on macOS')
    expect(w.text()).toContain('10.0.0.9')
    expect(w.text()).toContain('AB12-CD34')
    expect(w.find('[data-test="approve"]').exists()).toBe(true)
  })

  it('approves the device', async () => {
    get.mockResolvedValue(LOOKUP)
    post.mockResolvedValue(undefined)
    const w = mountView()
    await enterCodeAndLookup(w)
    await w.find('[data-test="approve"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/devices/pair/approve', { code: 'AB12-CD34' })
    expect(w.text()).toContain(en.devices.approved)
  })

  it('cancels the pairing and returns to entry', async () => {
    get.mockResolvedValue(LOOKUP)
    post.mockResolvedValue(undefined)
    const w = mountView()
    await enterCodeAndLookup(w)
    await w.find('[data-test="cancel"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/devices/pair/cancel', { code: 'AB12-CD34' })
    expect(w.find('[data-test="approve"]').exists()).toBe(false)
    expect(w.find('input[name="code"]').exists()).toBe(true)
  })

  it('keeps the confirmation card and shows an error when cancel fails', async () => {
    get.mockResolvedValue(LOOKUP)
    post.mockRejectedValue({ code: 'pairing_state', message: 'zh' })
    const w = mountView()
    await enterCodeAndLookup(w)
    await w.find('[data-test="cancel"]').trigger('click'); await flushPromises()
    expect(w.find('[data-test="approve"]').exists()).toBe(true)
    expect(w.text()).toContain(en.errors.pairing_state)
  })

  it('shows an already-approved note with no approve button and no cancel', async () => {
    get.mockResolvedValue({ ...LOOKUP, alreadyBound: true })
    const w = mountView()
    await enterCodeAndLookup(w)
    expect(w.text()).toContain(en.devices.alreadyBound)
    expect(w.find('[data-test="approve"]').exists()).toBe(false)
    expect(w.find('[data-test="cancel"]').exists()).toBe(false)
    expect(w.find('[data-test="done"]').exists()).toBe(true)
  })

  it('alreadyBound Done button resets to idle lookup state', async () => {
    get.mockResolvedValue({ ...LOOKUP, alreadyBound: true })
    const w = mountView()
    await enterCodeAndLookup(w)
    expect(w.find('[data-test="done"]').exists()).toBe(true)
    await w.find('[data-test="done"]').trigger('click')
    await flushPromises()
    // Should return to entry card (code input visible, confirm card gone)
    expect(w.find('input[name="code"]').exists()).toBe(true)
    expect(w.find('[data-test="done"]').exists()).toBe(false)
    expect(w.find('[data-test="approve"]').exists()).toBe(false)
  })

  it('surfaces pairing_not_found on a bad code', async () => {
    get.mockRejectedValue({ code: 'pairing_not_found', message: 'zh' })
    const w = mountView()
    await enterCodeAndLookup(w, 'ZZZZ-ZZZZ')
    expect(w.text()).toContain(en.errors.pairing_not_found)
  })
})
