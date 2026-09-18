import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import PasskeysCard from './PasskeysCard.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/webauthn', () => ({
  passkeyGet: vi.fn(), isUserCancel: () => false,
  passkeyRegister: vi.fn(async () => ({ id: 'newcred', response: {} })),
}))
const get = vi.mocked(api.get); const post = vi.mocked(api.post)
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const CREDS = [
  { id: 1, credentialIdSuffix: 'ab12', nickname: 'Laptop', transports: ['internal'], backupState: true, attestationType: 'none', createdAt: '2026-01-01T00:00:00Z' },
  { id: 2, credentialIdSuffix: 'cd34', transports: ['usb'], backupState: false, attestationType: 'none', createdAt: '2026-01-02T00:00:00Z' },
]
beforeEach(() => { get.mockReset(); post.mockReset() })
const mountCard = () => mount(PasskeysCard, { global: { plugins: [i18n()] }, attachTo: document.body })

describe('PasskeysCard', () => {
  it('lists passkeys with a fallback name and backup badge', async () => {
    get.mockResolvedValue(CREDS)
    const w = mountCard(); await flushPromises()
    expect(w.text()).toContain('Laptop')
    expect(w.text()).toContain('····cd34')
  })

  it('shows configured badge in header after load', async () => {
    get.mockResolvedValue(CREDS)
    const w = mountCard(); await flushPromises()
    // Badge shows count; "2 configured"
    expect(w.text()).toContain('2 configured')
  })

  it('shows not-configured badge in header when no passkeys', async () => {
    get.mockResolvedValue([])
    const w = mountCard(); await flushPromises()
    expect(w.text()).toContain(en.security.passkeys.notConfigured)
  })

  it('adds a passkey (begin → register → complete) then reloads', async () => {
    get.mockResolvedValue(CREDS)
    post.mockImplementation(async (p: string) => p.endsWith('/register/begin') ? { challenge: 'c' } : undefined)
    const w = mountCard(); await flushPromises()
    // Add button is now in the card body
    const addBtn = w.findAll('button').find((b) => b.text().includes(en.security.passkeys.add))!
    await addBtn.trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/credentials/register/begin')
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/credentials/register/complete', expect.objectContaining({ id: 'newcred' }))
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('renames a passkey and reloads', async () => {
    get.mockResolvedValue(CREDS)
    post.mockResolvedValue(undefined)
    const w = mountCard(); await flushPromises()
    const renameBtn = w.findAll('button').find((b) => b.text() === en.security.passkeys.rename)!
    await renameBtn.trigger('click'); await flushPromises()
    const nameInput = w.find<HTMLInputElement>('input[name="nickname"]')
    await nameInput.setValue('New Name')
    const saveBtn = w.findAll('button').find((b) => b.text() === en.security.passkeys.save)!
    await saveBtn.trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/credentials/rename', { id: 1, nickname: 'New Name' })
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('renders the last_passkey error from a failed delete', async () => {
    get.mockResolvedValue([CREDS[0]])
    post.mockRejectedValue({ code: 'last_passkey', message: '…' })
    const w = mountCard(); await flushPromises()
    await w.find('[aria-label="' + en.security.passkeys.remove + '"]').trigger('click')
    await flushPromises()
    const confirmBtn = Array.from(document.body.querySelectorAll('button')).find((b) => b.getAttribute('data-variant') === 'destructive')!
    confirmBtn.click(); await flushPromises()
    expect(w.text()).toContain(en.errors.codes.last_passkey)
  })

  it('shows empty state when loaded with no passkeys', async () => {
    get.mockResolvedValue([])
    const w = mountCard(); await flushPromises()
    expect(w.text()).toContain(en.security.passkeys.empty)
  })

  it('rename input has aria-label', async () => {
    get.mockResolvedValue(CREDS)
    const w = mountCard(); await flushPromises()
    const renameBtn = w.findAll('button').find((b) => b.text() === en.security.passkeys.rename)!
    await renameBtn.trigger('click'); await flushPromises()
    const input = w.find<HTMLInputElement>('input[name="nickname"]')
    expect(input.attributes('aria-label')).toBe(en.security.passkeys.rename)
  })
})
