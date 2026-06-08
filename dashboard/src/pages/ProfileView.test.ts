import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import en from '@/locales/en'
import ProfileView from './ProfileView.vue'
import { useAuthStore } from '@/stores/auth'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const put = vi.mocked(api.put)

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

beforeEach(() => {
  setActivePinia(createPinia())
  put.mockReset()
})

describe('ProfileView', () => {
  it('renders the profile fields read-only (no inputs or buttons initially)', () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'admin' }
    const wrapper = mount(ProfileView, { global: { plugins: [makeI18n()] } })
    expect(wrapper.text()).toContain('alex')
    expect(wrapper.text()).toContain('Alex Smith')
    expect(wrapper.text()).toContain('admin')
    expect(wrapper.find('input').exists()).toBe(false)
    expect(wrapper.find('[data-test="profile-save"]').exists()).toBe(false)
  })

  it('clicking Edit shows the input seeded with current displayName', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const wrapper = mount(ProfileView, { global: { plugins: [makeI18n()] } })
    await wrapper.find('[data-test="profile-edit"]').trigger('click')
    const input = wrapper.find('[data-test="profile-displayName-input"]')
    expect(input.exists()).toBe(true)
    expect((input.element as HTMLInputElement).value).toBe('Alex Smith')
  })

  it('Cancel exits edit mode without calling PUT', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const wrapper = mount(ProfileView, { global: { plugins: [makeI18n()] } })
    await wrapper.find('[data-test="profile-edit"]').trigger('click')
    await wrapper.find('[data-test="profile-cancel"]').trigger('click')
    expect(wrapper.find('input').exists()).toBe(false)
    expect(put).not.toHaveBeenCalled()
  })

  it('Save calls PUT /me with draft, patches the store from RESPONSE not draft, exits edit mode', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    // Response returns a DIFFERENT value than the typed draft to prove store is patched from the
    // response, not from draft.value.
    put.mockResolvedValue({ id: 1, username: 'alex', displayName: 'ALEXANDER SMITH', role: 'user' })
    const wrapper = mount(ProfileView, { global: { plugins: [makeI18n()] } })
    await wrapper.find('[data-test="profile-edit"]').trigger('click')
    const input = wrapper.find('[data-test="profile-displayName-input"]')
    await input.setValue('Alexander Smith')
    await wrapper.find('[data-test="profile-save"]').trigger('click')
    await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/me', { displayName: 'Alexander Smith' })
    // Store must reflect the SERVER'S response value, not the locally-typed draft.
    expect(auth.me?.displayName).toBe('ALEXANDER SMITH')
    expect(wrapper.find('input').exists()).toBe(false)
  })

  it('surfaces a validation error (invalid_display_name) from the API via Alert', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    put.mockRejectedValue({ code: 'invalid_display_name', message: 'zh message' })
    const wrapper = mount(ProfileView, { global: { plugins: [makeI18n()] } })
    await wrapper.find('[data-test="profile-edit"]').trigger('click')
    await wrapper.find('[data-test="profile-save"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain(en.errors.invalid_display_name)
    // Still in edit mode after error
    expect(wrapper.find('input').exists()).toBe(true)
  })

  it('username and role remain read-only in edit mode', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'admin' }
    const wrapper = mount(ProfileView, { global: { plugins: [makeI18n()] } })
    await wrapper.find('[data-test="profile-edit"]').trigger('click')
    // Only one input (displayName) — username is still a <dd> text node
    expect(wrapper.findAll('input')).toHaveLength(1)
    expect(wrapper.text()).toContain('alex')
    expect(wrapper.text()).toContain('admin')
  })
})
