import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia } from 'pinia'
import en from '@/locales/en'
import LogoutView from './LogoutView.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }))
import { api } from '@/lib/api'
const post = vi.mocked(api.post)

// RouterLink stub — avoids needing a full router install
const RouterLink = { template: '<a><slot /></a>', props: ['to'] }

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
function mountView() {
  return mount(LogoutView, {
    global: { plugins: [i18n(), createPinia()], stubs: { RouterLink, CenteredLayout: { template: '<div><slot name="title" /><slot /></div>' } } },
  })
}

beforeEach(() => { post.mockReset() })

describe('LogoutView', () => {
  it('renders the title and clarified scope message', async () => {
    post.mockResolvedValueOnce(undefined)
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain(en.logout.title)
    expect(w.text()).toContain(en.logout.message)
  })

  it('the message mentions the identity provider scope limitation', async () => {
    post.mockResolvedValueOnce(undefined)
    const w = mountView()
    await flushPromises()
    // The updated message must communicate that only the IdP session ended
    expect(en.logout.message).toContain('identity provider')
    expect(en.logout.message).toContain('separately')
    expect(w.text()).toContain(en.logout.signInAgain)
  })
})
