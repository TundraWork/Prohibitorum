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
  return mount(PasswordTotpForm, { global: { plugins: [makeI18n()] }, props: { returnTo: '/me/security' } })
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
    post.mockResolvedValueOnce({ redirect: '/me/security' }) // totp/verify → 200 { redirect }
    const wrapper = mountForm()

    await wrapper.find('input[name=username]').setValue('alex')
    await wrapper.find('input[name=password]').setValue('hunter2')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    await wrapper.find('input[name=code]').setValue('123456')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(post).toHaveBeenLastCalledWith('/api/prohibitorum/auth/totp/verify?return_to=%2Fme%2Fsecurity', {
      partial_session_token: 'pt_123',
      code: '123456',
    })
    expect(wrapper.emitted('success')).toBeTruthy()
    expect(wrapper.emitted('success')![0]).toEqual(['/me/security'])
  })

  it('renders a mapped error message and does not advance on a failed begin', async () => {
    post.mockRejectedValueOnce({ code: 'bad_credentials', message: 'nope' })
    const wrapper = mountForm()

    await wrapper.find('input[name=username]').setValue('alex')
    await wrapper.find('input[name=password]').setValue('wrong')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.find('[role=alert]').text()).toBe(en.errors.codes.bad_credentials)
    expect(wrapper.find('input[name=code]').exists()).toBe(false)
  })

  it('reveals the recovery sub-flow from the TOTP step', async () => {
    post.mockResolvedValueOnce({ partial_session_token: 'pt_123' })
    const w = mountForm()
    await w.find('input[name=username]').setValue('alex')
    await w.find('input[name=password]').setValue('hunter2')
    await w.find('form').trigger('submit'); await flushPromises()
    expect(w.find('[data-test="lost-authenticator"]').exists()).toBe(true)
    await w.find('[data-test="lost-authenticator"]').trigger('click')
    expect(w.find('input[name="recovery-code"]').exists()).toBe(true) // AccountRecovery mounted
  })
  it('returns to the password step when recovery restarts', async () => {
    post.mockImplementation(async (p: string) => {
      if (p.endsWith('/auth/password/begin')) return { partial_session_token: 'pt_1' }
      if (p.endsWith('/auth/recovery-code/verify')) throw { code: 'bad_credentials', message: 'zh' }
      return undefined
    })
    const w = mountForm()
    await w.find('input[name=username]').setValue('alex')
    await w.find('input[name=password]').setValue('hunter2')
    await w.find('form').trigger('submit'); await flushPromises()
    await w.find('[data-test="lost-authenticator"]').trigger('click')
    await w.find('input[name="recovery-code"]').setValue('wrong')
    await w.find('[data-test="verify-code"]').trigger('click'); await flushPromises()
    expect(w.find('input[name=username]').exists()).toBe(true) // back to password phase
    expect(w.text()).toContain(en.login.recoveryRestart)
  })
})
