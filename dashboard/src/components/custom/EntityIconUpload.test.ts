import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), upload: vi.fn(), del: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const upload = vi.mocked(api.upload)
const del = vi.mocked(api.del)
import EntityIconUpload from './EntityIconUpload.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

const mountComp = (iconUrl?: string | null) =>
  mount(EntityIconUpload, {
    props: { basePath: '/api/prohibitorum/oidc-applications/my-app', name: 'My App', iconUrl },
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })

beforeEach(() => { upload.mockReset(); del.mockReset() })

describe('EntityIconUpload', () => {
  it('calls api.upload with the correct path and file, then emits changed', async () => {
    upload.mockResolvedValue(undefined)
    const w = mountComp()
    const file = new File(['img'], 'icon.png', { type: 'image/png' })
    const input = w.find<HTMLInputElement>('[data-test="icon-input"]')
    // Simulate file selection by setting files on the input and triggering change
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    await input.trigger('change')
    await flushPromises()
    expect(upload).toHaveBeenCalledWith('/api/prohibitorum/oidc-applications/my-app/icon', file)
    expect(w.emitted('changed')).toBeTruthy()
  })

  it('does not render the Remove button when iconUrl is not set', () => {
    const w = mountComp()
    expect(w.find('[data-test="icon-remove"]').exists()).toBe(false)
  })

  it('renders the Remove button when iconUrl is set', () => {
    const w = mountComp('/icon/oidc_client/my-app?v=1')
    expect(w.find('[data-test="icon-remove"]').exists()).toBe(true)
  })

  it('calls api.del with the correct path and emits changed on remove', async () => {
    del.mockResolvedValue(undefined)
    const w = mountComp('/icon/oidc_client/my-app?v=1')
    await w.find('[data-test="icon-remove"]').trigger('click')
    await flushPromises()
    expect(del).toHaveBeenCalledWith('/api/prohibitorum/oidc-applications/my-app/icon')
    expect(w.emitted('changed')).toBeTruthy()
  })

  it('shows an error alert and does not emit changed when upload fails', async () => {
    // An app 4xx (e.g. a rejected image) still renders inline; connectivity/5xx
    // and unexpected non-ApiError throws (mapped to server_error) are now
    // surfaced via the global toast instead.
    upload.mockRejectedValue({ code: 'avatar_invalid_image', message: 'zh' })
    const w = mountComp()
    const file = new File(['img'], 'icon.png', { type: 'image/png' })
    const input = w.find<HTMLInputElement>('[data-test="icon-input"]')
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    await input.trigger('change')
    await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(true)
    expect(w.emitted('changed')).toBeFalsy()
  })
})
