import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }))
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => unknown) => fn() }))
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))

import { api } from '@/lib/api'
import AdminForwardAuthAppsView from './AdminForwardAuthAppsView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminForwardAuthAppsView, { global: { plugins: [i18n()], stubs: { RouterLink: true } } })

describe('AdminForwardAuthAppsView', () => {
  beforeEach(() => { vi.clearAllMocks(); push.mockReset() })

  it('lists forward-auth services', async () => {
    ;(api.get as ReturnType<typeof vi.fn>).mockResolvedValue([
      { clientId: 'fa1', displayName: 'App One', forwardAuthHost: 'app.example.test', accessRestricted: false, disabled: false, createdAt: '' },
    ])
    const w = mountView()
    await flushPromises()
    expect(w.html()).toContain('App One')
    expect(w.html()).toContain('app.example.test')
  })

  it('shows the empty state when there are no services', async () => {
    ;(api.get as ReturnType<typeof vi.fn>).mockResolvedValue([])
    const w = mountView()
    await flushPromises()
    expect(w.html()).toContain(en.admin.forwardAuth.empty)
  })

  it('row click navigates to detail', async () => {
    ;(api.get as ReturnType<typeof vi.fn>).mockResolvedValue([
      { clientId: 'fa1', displayName: 'App One', forwardAuthHost: 'app.example.test', accessRestricted: false, disabled: false, createdAt: '' },
    ])
    const w = mountView()
    await flushPromises()
    await w.find('[data-test="fa-row-fa1"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/forward-auth-apps/fa1')
  })

  it('registers a new service and reloads the list', async () => {
    ;(api.get as ReturnType<typeof vi.fn>).mockResolvedValue([])
    ;(api.post as ReturnType<typeof vi.fn>).mockResolvedValue(
      { clientId: 'new-svc', displayName: 'New Service', forwardAuthHost: 'new.example.test', accessRestricted: false, disabled: false, createdAt: '' },
    )
    const w = mountView()
    await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('input[name="clientId"]').setValue('new-svc')
    await w.find('input[name="host"]').setValue('new.example.test')
    await w.find('input[name="displayName"]').setValue('New Service')
    // reload returns the new service
    ;(api.get as ReturnType<typeof vi.fn>).mockResolvedValue([
      { clientId: 'new-svc', displayName: 'New Service', forwardAuthHost: 'new.example.test', accessRestricted: false, disabled: false, createdAt: '' },
    ])
    await w.find('[data-test="create-confirm"]').trigger('click')
    await flushPromises()
    expect(api.post).toHaveBeenCalledWith('/api/prohibitorum/forward-auth-apps', expect.objectContaining({
      clientId: 'new-svc', host: 'new.example.test', displayName: 'New Service',
    }))
    expect(w.html()).toContain(en.admin.forwardAuth.created)
  })
})
