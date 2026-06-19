import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent } from 'vue'
import en from '@/locales/en'
import DashboardLayout from './DashboardLayout.vue'

// --- Mocks ----------------------------------------------------------------

// Auth store — no-op ensureLoaded, never a real network call.
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ ensureLoaded: vi.fn() }),
}))

// Sidebar primitives — stub them so the test doesn't need the full sidebar tree.
vi.mock('@/components/ui/sidebar', () => ({
  SidebarProvider: defineComponent({ template: '<div><slot/></div>' }),
  SidebarInset: defineComponent({ template: '<main><slot/></main>' }),
  SidebarTrigger: defineComponent({ template: '<button/>' }),
}))

// AppSidebar, SudoModal — not under test here.
vi.mock('@/components/custom/AppSidebar.vue', () => ({
  default: defineComponent({ template: '<div/>' }),
}))
vi.mock('@/components/custom/SudoModal.vue', () => ({
  default: defineComponent({ template: '<div/>' }),
}))

// vue-router — each test creates a fresh layout with a specific route name.
// We mock useRoute() to return an object with the given name string directly.
let _routeName: string | null = null
vi.mock('vue-router', () => ({
  useRoute: () => ({ name: _routeName }),
}))

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

function mountLayout(name: string) {
  _routeName = name
  return mount(DashboardLayout, {
    global: {
      plugins: [makeI18n()],
      stubs: { RouterView: { template: '<div/>' } },
    },
  })
}

describe('DashboardLayout — header page title', () => {
  it('shows the Security title for the security route', () => {
    const w = mountLayout('security')
    expect(w.find('header p').text()).toBe(en.nav.security)
  })

  it('shows the Sessions title for the sessions route', () => {
    const w = mountLayout('sessions')
    expect(w.find('header p').text()).toBe(en.nav.sessions)
  })

  it('shows the Accounts title for admin-accounts route', () => {
    const w = mountLayout('admin-accounts')
    expect(w.find('header p').text()).toBe(en.admin.nav.accounts)
  })

  it('shows the Accounts title for admin-account-detail (falls back to parent section)', () => {
    const w = mountLayout('admin-account-detail')
    expect(w.find('header p').text()).toBe(en.admin.nav.accounts)
  })

  it('shows the Audit log title for admin-audit route', () => {
    const w = mountLayout('admin-audit')
    expect(w.find('header p').text()).toBe(en.admin.nav.audit)
  })

  it('renders no h1 for an unmapped route', () => {
    const w = mountLayout('unknown-route')
    expect(w.find('header p').exists()).toBe(false)
  })
})
