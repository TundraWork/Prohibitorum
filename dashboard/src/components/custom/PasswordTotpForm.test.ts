import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import PasswordTotpForm from './PasswordTotpForm.vue'

// Mock the typed api client; the form is the unit under test, not the network.
vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn() },
}))
import { api } from '@/lib/api'

const post = vi.mocked(api.post)

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

function mountForm() {
  return mount(PasswordTotpForm, { global: { plugins: [makeI18n()] } })
}

beforeEach(() => {
  post.mockReset()
})

describe('PasswordTotpForm', () => {
  it('phase 1 posts password/begin and advances to the TOTP step', async () => {
    post.mockResolvedValueOnce({ partial_session_token: 'pt_123' })
    const wrapper = mountForm()

    await wrapper.find('input[name=username]').setValue('alex')
    await wrapper.find('input[name=password]').setValue('hunter2')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/password/begin', {
      username: 'alex',
      password: 'hunter2',
    })
    // UI effect: the TOTP field appears, the password field is gone.
    expect(wrapper.find('input[name=code]').exists()).toBe(true)
    expect(wrapper.find('input[name=password]').exists()).toBe(false)
  })

  it('phase 2 posts totp/verify with the partial token and emits success', async () => {
    post.mockResolvedValueOnce({ partial_session_token: 'pt_123' }) // begin
    post.mockResolvedValueOnce(undefined) // totp/verify → 204 No Content
    const wrapper = mountForm()

    await wrapper.find('input[name=username]').setValue('alex')
    await wrapper.find('input[name=password]').setValue('hunter2')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    await wrapper.find('input[name=code]').setValue('123456')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(post).toHaveBeenLastCalledWith('/api/prohibitorum/auth/totp/verify', {
      partial_session_token: 'pt_123',
      code: '123456',
    })
    expect(wrapper.emitted('success')).toBeTruthy()
  })

  it('renders a mapped error message and does not advance on a failed begin', async () => {
    post.mockRejectedValueOnce({ code: 'invalid_credentials', message: 'nope' })
    const wrapper = mountForm()

    await wrapper.find('input[name=username]').setValue('alex')
    await wrapper.find('input[name=password]').setValue('wrong')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.find('[role=alert]').text()).toBe(en.errors.invalid_credentials)
    expect(wrapper.find('input[name=code]').exists()).toBe(false)
  })
})
