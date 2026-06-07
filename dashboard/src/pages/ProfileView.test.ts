import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import en from '@/locales/en'
import ProfileView from './ProfileView.vue'
import { useAuthStore } from '@/stores/auth'

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}
beforeEach(() => setActivePinia(createPinia()))

describe('ProfileView', () => {
  it('renders the profile fields read-only', () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'admin' }
    const wrapper = mount(ProfileView, { global: { plugins: [makeI18n()] } })
    expect(wrapper.text()).toContain('alex')
    expect(wrapper.text()).toContain('Alex Smith')
    expect(wrapper.text()).toContain('admin')
    expect(wrapper.find('input').exists()).toBe(false)
    expect(wrapper.find('button').exists()).toBe(false)
  })
})
