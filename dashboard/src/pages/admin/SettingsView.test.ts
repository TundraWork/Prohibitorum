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

const get = vi.mocked(api.get)
const put = vi.mocked(api.put)
const del = vi.mocked(api.del)

const defaultClientIpCfg = { strategy: 'direct', header: '', trustedProxies: [] }

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

function setupGetMock(clientIpOverride?: object) {
  get.mockImplementation((url: string) => {
    if (url === '/api/prohibitorum/admin/settings/client-ip') {
      return Promise.resolve(clientIpOverride ?? defaultClientIpCfg)
    }
    // branding store's ensureLoaded calls /config — return undefined to leave store state as patched
    return Promise.resolve(undefined)
  })
}

function mountView(hasCustomIcon = false, clientIpOverride?: object) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const branding = useBrandingStore()
  branding.$patch({ instanceName: 'TestInstance', hasCustomIcon, iconSrc: '/api/prohibitorum/icon', hasCustomBackground: hasCustomIcon, backgroundSrc: '/branding/background' })
  setupGetMock(clientIpOverride)
  return mount(SettingsView, { global: { plugins: [i18n(), pinia] }, attachTo: document.body })
}

beforeEach(() => {
  get.mockReset()
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

  it('renders the Upload background button', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-test="upload-background"]').exists()).toBe(true)
  })

  it('does not show Remove background when hasCustomBackground is false', async () => {
    const w = mountView(false)
    await flushPromises()
    expect(w.find('[data-test="remove-background"]').exists()).toBe(false)
  })

  it('shows Remove background when set and calls api.del on click', async () => {
    del.mockResolvedValue({})
    const w = mountView(true)
    await flushPromises()
    const btn = w.find('[data-test="remove-background"]')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    await flushPromises()
    expect(del).toHaveBeenCalledWith('/api/prohibitorum/admin/settings/background')
  })

  describe('Client IP / Proxy card', () => {
    it('loads strategy=header and renders header input + trusted textarea with loaded values', async () => {
      const w = mountView(false, { strategy: 'header', header: 'CF-Connecting-IP', trustedProxies: ['203.0.113.0/24'] })
      await flushPromises()
      const headerInput = w.find('[data-test="client-ip-header"]')
      expect(headerInput.exists()).toBe(true)
      expect((headerInput.element as HTMLInputElement).value).toBe('CF-Connecting-IP')
      const trustedArea = w.find('[data-test="client-ip-trusted"]')
      expect(trustedArea.exists()).toBe(true)
      expect((trustedArea.element as HTMLTextAreaElement).value).toBe('203.0.113.0/24')
    })

    it('hides header input and trusted textarea when strategy is direct', async () => {
      const w = mountView(false, { strategy: 'direct', header: '', trustedProxies: [] })
      await flushPromises()
      expect(w.find('[data-test="client-ip-header"]').exists()).toBe(false)
      expect(w.find('[data-test="client-ip-trusted"]').exists()).toBe(false)
    })

    it('shows fail-safe warning when strategy is header and trusted textarea is empty', async () => {
      const w = mountView(false, { strategy: 'header', header: 'CF-Connecting-IP', trustedProxies: [] })
      await flushPromises()
      expect(w.find('[data-test="client-ip-warning"]').exists()).toBe(true)
    })

    it('does not show warning when trusted textarea has content', async () => {
      const w = mountView(false, { strategy: 'header', header: 'CF-Connecting-IP', trustedProxies: ['10.0.0.0/8'] })
      await flushPromises()
      expect(w.find('[data-test="client-ip-warning"]').exists()).toBe(false)
    })

    it('saves via api.put with correct body through withSudo on click', async () => {
      put.mockResolvedValue({})
      const w = mountView(false, { strategy: 'forwarded', header: '', trustedProxies: ['10.0.0.0/8', '172.16.0.0/12'] })
      await flushPromises()
      await w.find('[data-test="save-client-ip"]').trigger('click')
      await flushPromises()
      expect(put).toHaveBeenCalledWith('/api/prohibitorum/admin/settings/client-ip', {
        strategy: 'forwarded',
        header: '',
        trustedProxies: ['10.0.0.0/8', '172.16.0.0/12'],
      })
    })

    it('strips blank lines and trims whitespace from trusted proxies on save', async () => {
      put.mockResolvedValue({})
      const w = mountView(false, { strategy: 'forwarded', header: '', trustedProxies: [] })
      await flushPromises()
      const area = w.find('[data-test="client-ip-trusted"]')
      await area.setValue('  10.0.0.0/8  \n\n  172.16.0.0/12  \n')
      await w.find('[data-test="save-client-ip"]').trigger('click')
      await flushPromises()
      expect(put).toHaveBeenCalledWith('/api/prohibitorum/admin/settings/client-ip', {
        strategy: 'forwarded',
        header: '',
        trustedProxies: ['10.0.0.0/8', '172.16.0.0/12'],
      })
    })

    it('sends empty header string when strategy is not header', async () => {
      put.mockResolvedValue({})
      const w = mountView(false, { strategy: 'header', header: 'CF-Connecting-IP', trustedProxies: ['10.0.0.0/8'] })
      await flushPromises()
      // Switch strategy to forwarded via the component's exposed reactive state
      const vm = w.getComponent(SettingsView).vm as { clientIpStrategy: string }
      vm.clientIpStrategy = 'forwarded'
      await flushPromises()
      await w.find('[data-test="save-client-ip"]').trigger('click')
      await flushPromises()
      expect(put).toHaveBeenCalledWith('/api/prohibitorum/admin/settings/client-ip', expect.objectContaining({
        strategy: 'forwarded',
        header: '',
      }))
    })
  })
})
