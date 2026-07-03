import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia } from 'pinia'
import en from '@/locales/en'
import MaintenanceView from './MaintenanceView.vue'

// vue-router stubs
vi.mock('vue-router', () => ({
  useRoute: () => ({ query: {} }),
  RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' },
}))

// Branding store: configurable per test
const brandingState = vi.hoisted(() => ({
  instanceName: 'Acme IdP',
  maintenanceMode: true,
  maintenanceMessage: '',
  ensureLoaded: vi.fn(async () => {}),
}))
vi.mock('@/stores/branding', () => ({ useBrandingStore: () => brandingState }))

// Auth store: configurable per test
const authState = vi.hoisted(() => ({
  me: null as null | { id: number; username: string },
  ensureLoaded: vi.fn(async () => {}),
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => authState }))

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

function mountView() {
  return mount(MaintenanceView, {
    global: {
      plugins: [makeI18n(), createPinia()],
      stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } },
    },
  })
}

beforeEach(() => {
  brandingState.instanceName = 'Acme IdP'
  brandingState.maintenanceMessage = ''
  authState.me = null
  authState.ensureLoaded.mockClear()
  authState.ensureLoaded.mockResolvedValue(undefined)
})

describe('MaintenanceView', () => {
  it('renders the warm heading', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('h1').text()).toBe(en.maintenance.heading)
  })

  it('renders the body text with the instance name', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('Acme IdP')
  })

  it('does NOT show the note callout when maintenanceMessage is empty', async () => {
    brandingState.maintenanceMessage = ''
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).not.toContain(en.maintenance.noteLabel)
  })

  it('shows the note callout with admin message when maintenanceMessage is set', async () => {
    brandingState.maintenanceMessage = 'Back at 17:00 UTC'
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain(en.maintenance.noteLabel)
    expect(wrapper.text()).toContain('Back at 17:00 UTC')
  })

  it('does NOT show the sign-out link when unauthenticated', async () => {
    authState.me = null
    const wrapper = mountView()
    await flushPromises()
    const links = wrapper.findAll('a')
    const logoutLink = links.find(l => l.attributes('href') === '/logout')
    expect(logoutLink).toBeUndefined()
  })

  it('shows the sign-out link when authenticated', async () => {
    authState.me = { id: 1, username: 'alex' }
    const wrapper = mountView()
    await flushPromises()
    const link = wrapper.findAll('a').find(l => l.attributes('href') === '/logout')
    expect(link).toBeDefined()
    expect(link!.text()).toBe(en.maintenance.signOut)
  })

  it('shows the admin-sign-in link when unauthenticated', async () => {
    authState.me = null
    const wrapper = mountView()
    await flushPromises()
    const link = wrapper.findAll('a').find(l => l.attributes('href') === '/login?admin=1')
    expect(link).toBeDefined()
    expect(link!.text()).toBe(en.maintenance.adminSignIn)
  })

  it('does NOT show the admin-sign-in link when authenticated', async () => {
    authState.me = { id: 1, username: 'alex' }
    const wrapper = mountView()
    await flushPromises()
    const link = wrapper.findAll('a').find(l => l.attributes('href') === '/login?admin=1')
    expect(link).toBeUndefined()
  })
})
