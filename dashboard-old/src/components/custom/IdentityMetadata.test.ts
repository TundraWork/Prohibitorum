import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import IdentityMetadata from './IdentityMetadata.vue'

const i18n = createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

function render(identity: {
  id: number
  providerSlug: string
  providerDisplayName: string
  protocol: string
  subject: string
  email?: string
  data: Record<string, string>
  linkedAt: string
}) {
  return mount(IdentityMetadata, { props: { identity }, global: { plugins: [i18n] } })
}

describe('IdentityMetadata', () => {
  it('maps each supported protocol to useful semantic labels', () => {
    const oidc = render({
      id: 1, providerSlug: 'work', providerDisplayName: 'Work', protocol: 'oidc',
      subject: '00u-subject', email: 'user@example.com', data: { ignored: 'hidden' }, linkedAt: '2026-01-01T00:00:00Z',
    })
    expect(oidc.text()).toContain(en.identity.subject)
    expect(oidc.text()).toContain('00u-subject')
    expect(oidc.text()).toContain(en.identity.email)
    expect(oidc.text()).toContain('user@example.com')

    const steam = render({
      id: 2, providerSlug: 'steam', providerDisplayName: 'Steam', protocol: 'steam',
      subject: '76561198000000000', data: { personaName: 'Player One', profileUrl: 'https://steamcommunity.com/id/player', ignored: 'hidden' }, linkedAt: '2026-01-01T00:00:00Z',
    })
    expect(steam.text()).toContain(en.identity.steamId)
    expect(steam.text()).toContain(en.identity.personaName)
    expect(steam.text()).toContain(en.identity.profileUrl)

    const vrchat = render({
      id: 3, providerSlug: 'vrchat', providerDisplayName: 'VRChat', protocol: 'vrchat',
      subject: 'usr_abcdefghijklmnop', data: { displayName: 'Avatar Name', profileUrl: 'https://vrchat.com/home/user/usr_abcdefghijklmnop', ignored: 'hidden' }, linkedAt: '2026-01-01T00:00:00Z',
    })
    expect(vrchat.text()).toContain(en.identity.vrchatUserId)
    expect(vrchat.text()).toContain(en.identity.displayName)
    expect(vrchat.text()).toContain(en.identity.profileUrl)
  })

  it('never renders unknown metadata keys or values', () => {
    const wrapper = render({
      id: 3, providerSlug: 'vrchat', providerDisplayName: 'VRChat', protocol: 'vrchat',
      subject: 'usr_known', data: { displayName: 'Known name', privateState: 'secret value' }, linkedAt: '2026-01-01T00:00:00Z',
    })
    expect(wrapper.text()).not.toContain('privateState')
    expect(wrapper.text()).not.toContain('secret value')
  })

  it('keeps exact IDs visually truncatable, monospace, titled, and fully accessible', () => {
    const id = 'usr_abcdefghijklmnopqrstuvwxyz0123456789'
    const wrapper = render({
      id: 3, providerSlug: 'vrchat', providerDisplayName: 'VRChat', protocol: 'vrchat',
      subject: id, data: {}, linkedAt: '2026-01-01T00:00:00Z',
    })
    const value = wrapper.get('[data-test="identity-userId"]')
    expect(value.classes()).toContain('truncate')
    expect(value.classes()).toContain('font-mono')
    expect(value.attributes('title')).toBe(id)
    expect(value.text()).toBe(id)
  })
})
