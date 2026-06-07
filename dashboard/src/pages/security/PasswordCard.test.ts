import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import PasswordCard from './PasswordCard.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => unknown) => fn(), ensureSudo: vi.fn(), sudoState: { value: { open: false, resolve: null } }, _resolveSudo: vi.fn() }))
const post = vi.mocked(api.post)
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
beforeEach(() => post.mockReset())
const mountCard = () => mount(PasswordCard, { global: { plugins: [i18n()] } })

describe('PasswordCard', () => {
  it('rejects a short password before calling the API', async () => {
    const w = mountCard()
    await w.find('input[name=new_password]').setValue('short')
    await w.find('input[name=confirm_password]').setValue('short')
    await w.find('form').trigger('submit'); await flushPromises()
    expect(w.text()).toContain(en.security.password.tooShort)
    expect(post).not.toHaveBeenCalled()
  })

  it('flags a mismatch', async () => {
    const w = mountCard()
    await w.find('input[name=new_password]').setValue('longenough1')
    await w.find('input[name=confirm_password]').setValue('different12')
    await w.find('form').trigger('submit'); await flushPromises()
    expect(w.text()).toContain(en.security.password.mismatch)
    expect(post).not.toHaveBeenCalled()
  })

  it('posts the password and shows success', async () => {
    post.mockResolvedValue(undefined)
    const w = mountCard()
    await w.find('input[name=new_password]').setValue('longenough1')
    await w.find('input[name=confirm_password]').setValue('longenough1')
    await w.find('form').trigger('submit'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/password/set', { password: 'longenough1' })
    expect(w.text()).toContain(en.security.password.saved)
  })
})
