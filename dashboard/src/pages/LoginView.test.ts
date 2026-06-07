import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import LoginView from './LoginView.vue'

// --- Mocks ----------------------------------------------------------------
vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn() },
}))
import { api } from '@/lib/api'

// WebAuthn ceremony: passkeyGet resolves a dummy assertion; never user-cancel.
vi.mock('@/lib/webauthn', () => ({
  passkeyGet: vi.fn(async () => ({ id: 'assert', response: {} })),
  passkeyRegister: vi.fn(),
  isUserCancel: () => false,
}))

// Return-to navigation: assert the guarded redirect fires (safeReturnTo itself
// is covered by returnTo.test.ts; here we only assert LoginView invokes it).
const { goReturnTo } = vi.hoisted(() => ({ goReturnTo: vi.fn() }))
vi.mock('@/composables/useReturnTo', () => ({
  useReturnTo: () => ({ returnTo: { value: '/me' }, goReturnTo }),
}))

const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

function mountView() {
  return mount(LoginView, { global: { plugins: [makeI18n()] } })
}

beforeEach(() => {
  get.mockReset()
  post.mockReset()
  goReturnTo.mockReset()
})

describe('LoginView', () => {
  it('completes a passkey login and navigates to the guarded return_to', async () => {
    get.mockImplementation(async (path: string) => {
      if (path === '/api/prohibitorum/auth/status') return { bootstrapped: true }
      if (path === '/api/prohibitorum/auth/federation') return []
      throw new Error(`unexpected GET ${path}`)
    })
    post.mockImplementation(async (path: string) => {
      if (path === '/api/prohibitorum/auth/login/begin') return { challenge: 'abc' }
      if (path === '/api/prohibitorum/auth/login/complete') return { id: 1, username: 'alex' }
      throw new Error(`unexpected POST ${path}`)
    })

    const wrapper = mountView()
    await flushPromises()

    const passkeyBtn = wrapper
      .findAll('button')
      .find((b) => b.text().includes(en.login.passkeyButton))
    expect(passkeyBtn, 'passkey button present').toBeTruthy()
    await passkeyBtn!.trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/login/begin')
    expect(post).toHaveBeenCalledWith(
      '/api/prohibitorum/auth/login/complete',
      expect.objectContaining({ id: 'assert' }),
    )
    expect(goReturnTo).toHaveBeenCalledOnce()
  })

  it('shows the enroll-admin hint when no admin exists yet', async () => {
    get.mockImplementation(async (path: string) => {
      if (path === '/api/prohibitorum/auth/status') return { bootstrapped: false }
      if (path === '/api/prohibitorum/auth/federation') return []
      throw new Error(`unexpected GET ${path}`)
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain(en.login.noBootstrap)
    const passkeyBtn = wrapper
      .findAll('button')
      .find((b) => b.text().includes(en.login.passkeyButton))
    expect(passkeyBtn).toBeFalsy()
  })
})
