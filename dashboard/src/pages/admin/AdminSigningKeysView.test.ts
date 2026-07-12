import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post)
import AdminSigningKeysView from './AdminSigningKeysView.vue'

const KEYS = [
  { kid: 'k-active', algorithm: 'RS256', use: 'sig', status: 'active', publicJwk: { kty: 'RSA', n: 'aaa' }, activatedAt: '2026-01-01T00:00:00Z' },
  { kid: 'k-pending', algorithm: 'RS256', use: 'sig', status: 'pending', publicJwk: { kty: 'RSA', n: 'bbb' } },
  { kid: 'k-decom', algorithm: 'RS256', use: 'sig', status: 'decommissioning', publicJwk: { kty: 'RSA', n: 'ccc' }, decommissionedAt: '2026-01-02T00:00:00Z' },
  { kid: 'k-retired', algorithm: 'RS256', use: 'sig', status: 'retired', publicJwk: { kty: 'RSA', n: 'ddd' }, retireAfter: '2026-02-01T00:00:00Z' },
]
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminSigningKeysView, { global: { plugins: [i18n()] }, attachTo: document.body })
const clickConfirm = async (label: string) => {
  const btns = Array.from(document.body.querySelectorAll('button')).filter((b) => b.textContent?.trim() === label && b.classList.contains('bg-destructive'))
  btns[btns.length - 1]!.click(); await flushPromises()
}
beforeEach(() => { get.mockReset(); post.mockReset() })

describe('AdminSigningKeysView', () => {
  it('lists keys with status badges', async () => {
    get.mockResolvedValue({ items: KEYS, nextCursor: '' })
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/signing-keys')
    expect(w.text()).toContain('k-active'); expect(w.text()).toContain(en.admin.signingKeys.statusActive)
    expect(w.text()).toContain(en.admin.signingKeys.statusPending); expect(w.text()).toContain(en.admin.signingKeys.statusDecommissioning)
  })
  it('generates a key via withSudo + confirm', async () => {
    get.mockResolvedValueOnce({ items: KEYS, nextCursor: '' }).mockResolvedValueOnce({ items: KEYS, nextCursor: '' })
    post.mockResolvedValue({ kid: 'k-new', status: 'pending' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="generate"]').trigger('click')
    await clickConfirm(en.admin.signingKeys.generateConfirm)
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/signing-keys/generate')
  })
  it('activates only a pending key', async () => {
    get.mockResolvedValue({ items: KEYS, nextCursor: '' }); post.mockResolvedValue({ kid: 'k-pending', status: 'active' })
    const w = mountView(); await flushPromises()
    expect(w.find('[data-test="activate-k-pending"]').exists()).toBe(true)
    expect(w.find('[data-test="activate-k-active"]').exists()).toBe(false)
    expect(w.find('[data-test="activate-k-retired"]').exists()).toBe(false)
    expect(w.find('[data-test="retire-k-retired"]').exists()).toBe(false)
    await w.find('[data-test="activate-k-pending"]').trigger('click')
    await clickConfirm(en.admin.signingKeys.activateConfirm)
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/signing-keys/k-pending/activate')
  })
  it('retires only a decommissioning key and surfaces active_key_no_replacement', async () => {
    get.mockResolvedValue({ items: KEYS, nextCursor: '' }); post.mockRejectedValue({ code: 'active_key_no_replacement', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.find('[data-test="retire-k-decom"]').exists()).toBe(true)
    expect(w.find('[data-test="retire-k-pending"]').exists()).toBe(false)
    expect(w.find('[data-test="activate-k-retired"]').exists()).toBe(false)
    expect(w.find('[data-test="retire-k-retired"]').exists()).toBe(false)
    await w.find('[data-test="retire-k-decom"]').trigger('click')
    await clickConfirm(en.admin.signingKeys.retireConfirm)
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/signing-keys/k-decom/retire')
    expect(w.text()).toContain(en.errors.codes.active_key_no_replacement)
  })
  it('describes the public JWK dialog with the selected key without warnings', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    try {
      get.mockResolvedValue({ items: KEYS, nextCursor: '' })
      const w = mountView(); await flushPromises()
      await w.find('[data-test="view-jwk-k-active"]').trigger('click')
      await flushPromises()

      const warnings = warn.mock.calls.flat().map(String).join('\n')
      expect(warnings).not.toContain('Missing `Description`')
      const dialog = document.body.querySelector('[role="dialog"]')
      const descriptionId = dialog?.getAttribute('aria-describedby')
      expect(descriptionId).toBeTruthy()
      const description = document.getElementById(descriptionId!)
      expect(description?.textContent).toContain('k-active')
      expect(description?.classList.contains('truncate')).toBe(false)
      const kid = description?.querySelector('[data-test="jwk-description-kid"]')
      expect(kid?.textContent).toBe('k-active')
      expect(kid?.classList.contains('break-all')).toBe(true)
      expect(kid?.classList.contains('font-mono')).toBe(true)
      expect(document.body.textContent).toContain('"kty": "RSA"')
    } finally {
      warn.mockRestore()
    }
  })
  it('shows empty-state when no keys', async () => {
    get.mockResolvedValue({ items: [], nextCursor: '' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.signingKeys.empty)
  })
})
