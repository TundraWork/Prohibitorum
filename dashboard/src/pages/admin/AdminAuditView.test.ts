import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
import AdminAuditView from './AdminAuditView.vue'

const page = (startId: number, n: number) => Array.from({ length: n }, (_, i) => ({
  id: startId - i, at: '2026-01-01T00:00:00Z', accountId: 7, factor: 'signing_key', event: 'activate',
  ip: '10.0.0.1', userAgent: 'curl', detail: { kid: `k${startId - i}`, action: 'activate' },
}))
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminAuditView, { global: { plugins: [i18n()] }, attachTo: document.body })
beforeEach(() => { get.mockReset() })

describe('AdminAuditView', () => {
  it('loads newest-first with limit=50', async () => {
    get.mockResolvedValue(page(100, 3))
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/audit-events?limit=50')
    expect(w.text()).toContain('activate')
  })
  it('applies filters and re-queries from the top', async () => {
    get.mockResolvedValue(page(100, 2))
    const w = mountView(); await flushPromises()
    await w.find('input[name="factor"]').setValue('signing_key')
    await w.find('input[name="event"]').setValue('activate')
    await w.find('[data-test="apply"]').trigger('click'); await flushPromises()
    const lastCall = get.mock.calls.at(-1)![0] as string
    expect(lastCall).toContain('factor=signing_key'); expect(lastCall).toContain('event=activate'); expect(lastCall).toContain('limit=50')
    expect(lastCall).not.toContain('before=')
  })
  it('load-more sends before=<lastId> and appends; hides when short page', async () => {
    get.mockResolvedValueOnce(page(100, 50)).mockResolvedValueOnce(page(50, 3))
    const w = mountView(); await flushPromises()
    expect(w.find('[data-test="load-more"]').exists()).toBe(true)
    await w.find('[data-test="load-more"]').trigger('click'); await flushPromises()
    expect((get.mock.calls.at(-1)![0] as string)).toContain('before=51')
    expect(w.find('[data-test="load-more"]').exists()).toBe(false)
    // appended, not replaced: 50 (page 1) + 3 (page 2) = 53 rows
    expect(w.findAll('[data-test^="expand-"]').length).toBe(53)
  })
  it('expands a row to show detail JSON', async () => {
    get.mockResolvedValue(page(100, 1))
    const w = mountView(); await flushPromises()
    await w.find('[data-test="expand-100"]').trigger('click')
    expect(w.text()).toContain('"action": "activate"')
  })
  it('shows empty-state when no events', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.audit.empty)
  })
})
