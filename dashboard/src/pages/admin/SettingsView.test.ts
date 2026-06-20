import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), put: vi.fn(), del: vi.fn(), upload: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
import { useBrandingStore } from '@/stores/branding'
import SettingsView from './SettingsView.vue'

const put = vi.mocked(api.put)
const del = vi.mocked(api.del)

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

function mountView(hasCustomIcon = false) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const branding = useBrandingStore()
  branding.$patch({ instanceName: 'TestInstance', hasCustomIcon, iconSrc: '/api/prohibitorum/icon' })
  return mount(SettingsView, { global: { plugins: [i18n(), pinia] }, attachTo: document.body })
}

beforeEach(() => {
  put.mockReset()
  del.mockReset()
})

describe('SettingsView', () => {
  it('renders the instance name input with the current name', async () => {
    const w = mountView()
    await flushPromises()
    const input = w.find('input#instance-name')
    expect(input.exists()).toBe(true)
    expect((input.element as HTMLInputElement).value).toBe('TestInstance')
  })

  it('saves the instance name via api.put', async () => {
    put.mockResolvedValue({})
    const w = mountView()
    await flushPromises()
    const input = w.find('input#instance-name')
    await input.setValue('NewName')
    await w.find('[data-test="save-name"]').trigger('click')
    await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/admin/settings', { instanceName: 'NewName' })
  })

  it('does not show Remove button when hasCustomIcon is false', async () => {
    const w = mountView(false)
    await flushPromises()
    expect(w.find('[data-test="remove-icon"]').exists()).toBe(false)
  })

  it('shows Remove button when hasCustomIcon is true and calls api.del on click', async () => {
    del.mockResolvedValue({})
    const w = mountView(true)
    await flushPromises()
    const removeBtn = w.find('[data-test="remove-icon"]')
    expect(removeBtn.exists()).toBe(true)
    await removeBtn.trigger('click')
    await flushPromises()
    expect(del).toHaveBeenCalledWith('/api/prohibitorum/admin/settings/icon')
  })

  it('renders the Upload icon button', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-test="upload-icon"]').exists()).toBe(true)
  })
})
