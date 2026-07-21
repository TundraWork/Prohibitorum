import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)

vi.mock('@/composables/useReturnTo', () => ({
  useReturnTo: () => ({ returnTo: ref('/me'), rawReturnTo: ref('/me'), goReturnTo: vi.fn() }),
}))

// vue-router is required by useReturnTo (even when mocked, the import is hoisted)
vi.mock('vue-router', () => ({ useRoute: () => ({ query: {} }) }))

import FederationButtons from './FederationButtons.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountComp = () => mount(FederationButtons, { global: { plugins: [i18n()] } })

const PROVIDERS_WITH_ICON = [
  { slug: 'google', displayName: 'Google', iconUrl: '/icon/upstream_idp/google?v=abc' },
  { slug: 'okta', displayName: 'Okta' },
]

const PROVIDERS_MIXED = [
  { slug: 'steamco', displayName: 'Steam', protocol: 'steam' },
  { slug: 'vrchat', displayName: 'VRChat', protocol: 'vrchat', iconUrl: '/ignored-admin-icon' },
  { slug: 'okta', displayName: 'Okta', protocol: 'oidc' },
]

beforeEach(() => { get.mockReset() })

describe('FederationButtons', () => {
  it('renders nothing while loading', () => {
    get.mockReturnValue(new Promise(() => {}))
    const w = mountComp()
    // Loading skeleton is present; no buttons yet
    expect(w.find('[role="status"]').exists()).toBe(true)
    expect(w.findAll('button')).toHaveLength(0)
  })

  it('renders nothing when provider list is empty', async () => {
    get.mockResolvedValue([])
    const w = mountComp(); await flushPromises()
    expect(w.findAll('button')).toHaveLength(0)
  })

  it('renders a button for each provider', async () => {
    get.mockResolvedValue(PROVIDERS_WITH_ICON)
    const w = mountComp(); await flushPromises()
    const buttons = w.findAll('button')
    expect(buttons).toHaveLength(2)
    expect(buttons[0]!.text()).toContain('Google')
    expect(buttons[1]!.text()).toContain('Okta')
  })

  it('shows an <img> inside the button when iconUrl is provided', async () => {
    get.mockResolvedValue(PROVIDERS_WITH_ICON)
    const w = mountComp(); await flushPromises()
    const buttons = w.findAll('button')
    // Google has iconUrl → AppIcon renders an <img>
    expect(buttons[0]!.find('img').exists()).toBe(true)
    expect(buttons[0]!.find('img').attributes('src')).toBe('/icon/upstream_idp/google?v=abc')
  })

  it('shows the initial letter fallback when iconUrl is absent', async () => {
    get.mockResolvedValue(PROVIDERS_WITH_ICON)
    const w = mountComp(); await flushPromises()
    const buttons = w.findAll('button')
    // Okta has no iconUrl → AppIcon renders the initial 'O', no <img>
    expect(buttons[1]!.find('img').exists()).toBe(false)
    expect(buttons[1]!.text()).toContain('O')
  })

  it('assigns window.location on button click', async () => {
    get.mockResolvedValue(PROVIDERS_WITH_ICON)
    const assign = vi.fn()
    vi.stubGlobal('location', { assign })
    const w = mountComp(); await flushPromises()
    await w.findAll('button')[0]!.trigger('click')
    expect(assign).toHaveBeenCalledWith(
      '/api/prohibitorum/auth/federation/google/login?return_to=%2Fme')
    vi.unstubAllGlobals()
  })

  it('renders Steam and VRChat with branded buttons and OIDC with a generic button', async () => {
    get.mockResolvedValue(PROVIDERS_MIXED)
    const w = mountComp(); await flushPromises()

    expect(w.findAll('[data-test="steam-login"]')).toHaveLength(1)
    expect(w.findAll('[data-test="vrchat-login"]')).toHaveLength(1)

    const genericButtons = w.findAll('button').filter((button) =>
      button.attributes('data-test') === undefined)
    expect(genericButtons).toHaveLength(1)
    expect(genericButtons[0]!.classes()).toContain('border')
    expect(genericButtons[0]!.text()).toContain('Okta')
  })

  it('renders VRChat with the predefined branded button', async () => {
    get.mockResolvedValue([
      { slug: 'vrchat', displayName: 'VRChat', protocol: 'vrchat', iconUrl: '/ignored-admin-icon' },
    ])
    const w = mountComp()
    await flushPromises()

    const button = w.get('[data-test="vrchat-login"]')
    expect(button.text()).toContain('VRChat')
    expect(button.classes()).toEqual(expect.arrayContaining(['bg-[#2BAAC1]', 'text-[#0B1A21]']))
    expect(button.classes()).toContain('active:bg-[#2592A6]')
    expect(button.find('img').attributes('src')).toContain('vrchat-logo')
    expect(button.find('img').attributes('alt')).toBe('')
    expect(button.text()).not.toContain('VVRChat')
  })
})
