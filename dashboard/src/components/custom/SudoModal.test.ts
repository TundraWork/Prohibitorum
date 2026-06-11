import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import SudoModal from './SudoModal.vue'
import { sudoState } from '@/lib/sudo'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/webauthn', () => ({
  passkeyGet: vi.fn(async () => ({ id: 'assert', response: {} })),
  passkeyRegister: vi.fn(),
  isUserCancel: () => false,
}))

const { hardRedirect } = vi.hoisted(() => ({ hardRedirect: vi.fn() }))
vi.mock('@/lib/navigate', () => ({ hardRedirect }))

vi.mock('vue-router', () => ({
  useRoute: () => ({ fullPath: '/dashboard' }),
}))

const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}
function mountModal() {
  return mount(SudoModal, { global: { plugins: [makeI18n()] }, attachTo: document.body })
}

beforeEach(() => { get.mockReset(); post.mockReset(); sudoState.value = { open: false, resolve: null } })

describe('SudoModal', () => {
  it('completes the passkey path and resolves true', async () => {
    get.mockResolvedValue({ methods: ['webauthn'] })
    post.mockImplementation(async (p: string) =>
      p.endsWith('/begin') ? { challenge: 'c' } : undefined)
    const resolve = vi.fn()
    mountModal()
    sudoState.value = { open: true, resolve }
    await flushPromises()
    const btn = document.querySelector('button')!
    ;(btn as HTMLButtonElement).click()
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/sudo/begin', { method: 'webauthn' })
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/sudo/complete', expect.objectContaining({ id: 'assert' }))
    expect(resolve).toHaveBeenCalledWith(true)
  })

  it('shows a terminal message when no method is available', async () => {
    get.mockResolvedValue({ methods: [] })
    mountModal()
    sudoState.value = { open: true, resolve: vi.fn() }
    await flushPromises()
    expect(document.body.textContent).toContain(en.sudo.noMethod)
  })

  it('cancel resolves false', async () => {
    get.mockResolvedValue({ methods: ['webauthn'] })
    const resolve = vi.fn()
    mountModal()
    sudoState.value = { open: true, resolve }
    await flushPromises()
    const buttons = Array.from(document.querySelectorAll('button'))
    const cancelBtn = buttons.find(b => b.textContent?.trim() === en.sudo.cancel)
    expect(cancelBtn).toBeTruthy()
    cancelBtn!.click()
    await flushPromises()
    expect(resolve).toHaveBeenCalledWith(false)
  })

  it('password+TOTP path resolves true', async () => {
    get.mockResolvedValue({ methods: ['password_totp'] })
    post.mockImplementation(async (p: string) =>
      p.endsWith('/begin') ? undefined : undefined)
    const resolve = vi.fn()
    mount(SudoModal, { global: { plugins: [makeI18n()] }, attachTo: document.body })
    sudoState.value = { open: true, resolve }
    await flushPromises()
    // showPwForm should be auto-true since only password_totp
    const passwordInput = document.querySelector('input[name=current_password]') as HTMLInputElement
    const totpInput = document.querySelector('input[name=totp_code]') as HTMLInputElement
    expect(passwordInput).toBeTruthy()
    expect(totpInput).toBeTruthy()
    passwordInput.value = 'secret'
    passwordInput.dispatchEvent(new Event('input'))
    totpInput.value = '123456'
    totpInput.dispatchEvent(new Event('input'))
    await flushPromises()
    const form = document.querySelector('form')!
    form.dispatchEvent(new Event('submit', { bubbles: true }))
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/sudo/begin', { method: 'password_totp' })
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/sudo/complete', {
      current_password: 'secret',
      totp_code: '123456',
    })
    expect(resolve).toHaveBeenCalledWith(true)
  })

  it('renders a provider button for each federation provider and redirects on click', async () => {
    get.mockResolvedValue({
      methods: ['federation_oidc'],
      federationProviders: [{ slug: 'google', displayName: 'Google' }],
    })
    post.mockResolvedValue({ redirect: 'https://accounts.google.com/o/oauth2/auth?step=1' })
    hardRedirect.mockReset()
    mountModal()
    sudoState.value = { open: true, resolve: vi.fn() }
    await flushPromises()

    const buttons = Array.from(document.querySelectorAll('button'))
    const googleBtn = buttons.find(b => b.textContent?.includes('Google'))
    expect(googleBtn).toBeTruthy()

    googleBtn!.click()
    await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/sudo/begin', {
      method: 'federation_oidc',
      slug: 'google',
      returnTo: '/dashboard',
    })
    expect(hardRedirect).toHaveBeenCalledWith('https://accounts.google.com/o/oauth2/auth?step=1')
  })
})
