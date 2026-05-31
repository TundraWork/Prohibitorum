import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import zh from '../locales/zh'
import en from '../locales/en'
import { useSessionStore } from '../stores/session'
import ProfileView from './ProfileView.vue'

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}
function makeRouter() {
  return createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div/>' } }, { path: '/logout', component: { template: '<div/>' } }] })
}

beforeEach(() => setActivePinia(createPinia()))

describe('ProfileView', () => {
  it('renders the current account', async () => {
    const s = useSessionStore()
    s.me = { id: 1, username: 'alice', displayName: 'Alice', role: 'admin' }
    const router = makeRouter()
    const wrapper = mount(ProfileView, { global: { plugins: [makeI18n(), router] } })
    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).toContain('Alice')
    expect(wrapper.text()).toContain('admin')
  })
})
