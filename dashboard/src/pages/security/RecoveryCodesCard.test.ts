import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import RecoveryCodesCard from './RecoveryCodesCard.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => unknown) => fn(), ensureSudo: vi.fn(), sudoState: { value: { open: false, resolve: null } }, _resolveSudo: vi.fn() }))
const post = vi.mocked(api.post)
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
beforeEach(() => { post.mockReset(); Object.assign(navigator, { clipboard: { writeText: vi.fn(async () => {}) } }) })
const mountCard = () => mount(RecoveryCodesCard, { global: { plugins: [i18n()] } })

describe('RecoveryCodesCard', () => {
  it('regenerates and displays codes', async () => {
    post.mockResolvedValue({ recovery_codes: ['x1', 'x2'] })
    const w = mountCard()
    await w.find('button').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/recovery-codes/regenerate')
    expect(w.findAll('li').map((l) => l.text())).toEqual(['x1', 'x2'])
  })

  it('shows the need-TOTP hint on bad_request', async () => {
    post.mockRejectedValue({ code: 'bad_request', message: '…' })
    const w = mountCard()
    await w.find('button').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.security.recovery.needTotp)
  })
})
