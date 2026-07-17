import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import en from '@/locales/en'
import VRChatProofView from './VRChatProofView.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }))
import { api } from '@/lib/api'

const mounted: VueWrapper[] = []
async function mountProof(proof: string): Promise<VueWrapper> {
  setActivePinia(createPinia())
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/verify/vrchat/:proof', component: VRChatProofView }],
  })
  await router.push(`/verify/vrchat/${proof}`)
  await router.isReady()
  const i18n = createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
  const wrapper = mount(VRChatProofView, { global: { plugins: [router, i18n] } })
  mounted.push(wrapper)
  return wrapper
}

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount()
  vi.mocked(api.get).mockReset()
  vi.mocked(api.post).mockReset()
})

describe('VRChatProofView', () => {
  it('makes no API call and states that visiting neither signs in nor approves access', async () => {
    const wrapper = await mountProof('proof_live')

    expect(api.get).not.toHaveBeenCalled()
    expect(api.post).not.toHaveBeenCalled()
    expect(wrapper.get('h1').text()).toBe('VRChat verification link')
    expect(wrapper.text()).toContain('Visiting this page does not sign anyone in, verify profile ownership, or approve access.')
    expect(wrapper.text()).not.toMatch(/valid|approved|verified owner/i)
    expect(wrapper.get('[data-test="locale-trigger"]').classes()).toContain('h-11')
  })

  it('gives the profile owner the same instructions for every token', async () => {
    const first = await mountProof('proof_live')
    const firstText = first.text()
    first.unmount()
    mounted.splice(mounted.indexOf(first), 1)

    const second = await mountProof('unknown_or_expired')
    expect(second.text()).toBe(firstText)
    expect(second.findAll('ol li')).toHaveLength(3)
    expect(second.text()).toContain('Return to Prohibitorum and press Verify profile.')
    expect(second.text()).toContain('Remove the link after Prohibitorum confirms verification.')
  })

  it('uses semantic ordered instructions with a single accessible page heading', async () => {
    const wrapper = await mountProof('proof_any')

    expect(wrapper.findAll('h1')).toHaveLength(1)
    expect(wrapper.get('ol').attributes('aria-label')).toBe('What the profile owner should do')
  })
})
