import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import en from '../locales/en'
import PasswordTotpForm from './PasswordTotpForm.vue'

// Mock the api module so the two-phase flow is driven by controllable resolves.
const post = vi.fn()
vi.mock('../lib/api', () => ({
  api: {
    get: (...a: unknown[]) => post(...a),
    post: (...a: unknown[]) => post(...a),
  },
}))

// The Nuxt UI Vite plugin (registered in vitest.config.ts) resolves <UButton>,
// <UInput>, <UFormField> so this real mount works without stubs.
function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}

function mountForm() {
  return mount(PasswordTotpForm, { global: { plugins: [makeI18n()] } })
}

beforeEach(() => {
  post.mockReset()
})

describe('PasswordTotpForm', () => {
  it('advances to the TOTP phase after password/begin resolves a partial token', async () => {
    post.mockResolvedValueOnce({ partial_session_token: 'tok' })
    const wrapper = mountForm()

    const inputs = wrapper.findAll('input')
    await inputs[0].setValue('alice')
    await inputs[1].setValue('hunter2')
    await wrapper.find('form').trigger('submit.prevent')
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    // password/begin was called with credentials.
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/password/begin', {
      username: 'alice',
      password: 'hunter2',
    })
    // A TOTP input is now present (phase advanced).
    expect(wrapper.findAll('input').length).toBe(1)
  })

  it('emits success after totp/verify resolves (204 -> undefined)', async () => {
    post.mockResolvedValueOnce({ partial_session_token: 'tok' })
    const wrapper = mountForm()

    let inputs = wrapper.findAll('input')
    await inputs[0].setValue('alice')
    await inputs[1].setValue('hunter2')
    await wrapper.find('form').trigger('submit.prevent')
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    // Phase 2: enter the code, totp/verify returns undefined (204).
    post.mockResolvedValueOnce(undefined)
    inputs = wrapper.findAll('input')
    await inputs[0].setValue('123456')
    await wrapper.find('form').trigger('submit.prevent')
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    expect(post).toHaveBeenLastCalledWith('/api/prohibitorum/auth/totp/verify', {
      partial_session_token: 'tok',
      code: '123456',
    })
    expect(wrapper.emitted('success')).toBeTruthy()
  })

  it('shows a localized error when password/begin rejects with a known code', async () => {
    post.mockRejectedValueOnce({ code: 'bad_credentials', message: 'nope' })
    const wrapper = mountForm()

    const inputs = wrapper.findAll('input')
    await inputs[0].setValue('alice')
    await inputs[1].setValue('wrong')
    await wrapper.find('form').trigger('submit.prevent')
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain(en.errors.bad_credentials)
    // Stayed on the credentials phase (2 inputs).
    expect(wrapper.findAll('input').length).toBe(2)
    expect(wrapper.emitted('success')).toBeFalsy()
  })
})
