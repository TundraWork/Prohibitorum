import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import SessionExpiredBanner from './SessionExpiredBanner.vue'
import { useSessionExpiry } from '@/composables/useSessionExpiry'

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push, currentRoute: { value: { fullPath: '/security' } } }),
}))

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

describe('SessionExpiredBanner', () => {
  beforeEach(() => { useSessionExpiry().reset(); push.mockClear() })

  it('renders nothing when not expired', () => {
    const w = mount(SessionExpiredBanner, { global: { plugins: [makeI18n()] } })
    expect(w.find('[role="alert"]').exists()).toBe(false)
  })

  it('shows the banner when expired is true', () => {
    useSessionExpiry().trigger()
    const w = mount(SessionExpiredBanner, { global: { plugins: [makeI18n()] } })
    expect(w.find('[role="alert"]').exists()).toBe(true)
    expect(w.text()).toContain(en.sessionExpiry.message)
    expect(w.text()).toContain(en.sessionExpiry.signInAgain)
  })

  it('navigates to login with return_to and reason on click, then resets the flag', async () => {
    useSessionExpiry().trigger()
    const w = mount(SessionExpiredBanner, { global: { plugins: [makeI18n()] } })
    await w.find('button').trigger('click')
    expect(push).toHaveBeenCalledWith({
      name: 'login',
      query: { return_to: '/security', reason: 'session_expired' },
    })
    expect(useSessionExpiry().expired.value).toBe(false)
  })
})
