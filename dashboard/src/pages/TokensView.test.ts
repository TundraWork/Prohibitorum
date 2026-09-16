import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)

import TokensView from './TokensView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

const TOKEN = {
  id: 1,
  name: 'ci-token',
  tokenHint: 'hint-abcd',
  allApps: true,
  appGrants: {},
  createdAt: '2026-01-01T00:00:00Z',
}

beforeEach(() => {
  get.mockReset()
  get.mockImplementation(async (p: string) =>
    p === '/api/prohibitorum/me/tokens' ? [TOKEN]
    : p === '/api/prohibitorum/me/forward-auth-apps' ? []
    : [])
})

describe('TokensView', () => {
  it('renders token rows on untitled (BareCard) cards', async () => {
    const w = mount(TokensView, { global: { plugins: [i18n()] } })
    await flushPromises()
    const name = w.find('[data-test="token-name"]')
    expect(name.exists()).toBe(true)
    expect(name.text()).toContain('ci-token')
    expect(name.element.closest('[data-slot="card"]')?.getAttribute('data-untitled')).toBe('true')
  })
})
