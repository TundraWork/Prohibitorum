import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post); const put = vi.mocked(api.put)
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { slug: 'okta' } }), useRouter: () => ({ push }), RouterLink: { template: '<a><slot/></a>' } }))
import AdminUpstreamIdpDetailView from './AdminUpstreamIdpDetailView.vue'

const IDP = { slug: 'okta', displayName: 'Okta', issuerUrl: 'https://okta/', clientId: 'c1', scopes: ['openid','email'], mode: 'auto_provision', allowedDomains: ['ex.com'], usernameClaim: 'preferred_username', displayNameClaim: 'name', emailClaim: 'email', pictureClaim: 'picture', requireVerifiedEmail: false, disabled: false, createdAt: '2026-01-01T00:00:00Z' }
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminUpstreamIdpDetailView, { global: { plugins: [i18n()], stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } } }, attachTo: document.body })
const clickConfirm = async (_w: ReturnType<typeof mountView>, label: string) => {
  const btns = Array.from(document.body.querySelectorAll('button')).filter((b) => b.textContent?.trim() === label && b.classList.contains('bg-destructive'))
  btns[btns.length - 1]!.click(); await flushPromises()
}
beforeEach(() => { get.mockReset(); post.mockReset(); put.mockReset(); push.mockReset() })

describe('AdminUpstreamIdpDetailView', () => {
  it('shows not-found on upstream_idp_not_found', async () => {
    get.mockRejectedValue({ code: 'upstream_idp_not_found', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.upstream.notFound)
  })
  it('saves config via PUT without clientSecret', async () => {
    get.mockResolvedValue(IDP); put.mockResolvedValue({ ...IDP, displayName: 'Okta 2' })
    const w = mountView(); await flushPromises()
    await w.find('input[name="displayName"]').setValue('Okta 2')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    const body = put.mock.calls[0][1] as Record<string, unknown>
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/okta', expect.objectContaining({ displayName: 'Okta 2', disabled: false }))
    expect(body).not.toHaveProperty('clientSecret')
    expect(w.text()).toContain(en.admin.upstream.saved)
  })
  it('includes pictureClaim in save payload and renders the input', async () => {
    get.mockResolvedValue(IDP); put.mockResolvedValue({ ...IDP, pictureClaim: 'avatar_url' })
    const w = mountView(); await flushPromises()
    expect(w.find('input[name="pictureClaim"]').exists()).toBe(true)
    await w.find('input[name="pictureClaim"]').setValue('avatar_url')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/okta', expect.objectContaining({ pictureClaim: 'avatar_url' }))
  })
  it('rotates the secret without revealing a value', async () => {
    get.mockResolvedValue(IDP); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('input[name="newSecret"]').setValue('newsek')
    await w.find('[data-test="rotate"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/rotate-secret', { slug: 'okta', clientSecret: 'newsek' })
    expect(w.text()).toContain(en.admin.upstream.rotated)
  })
  it('deletes and navigates back to the list', async () => {
    get.mockResolvedValue(IDP); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    await clickConfirm(w, en.admin.upstream.delete)
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/delete', { slug: 'okta' })
    expect(push).toHaveBeenCalledWith('/admin/identity-providers')
  })
  it('surfaces a generic load error in the Alert without showing not-found', async () => {
    get.mockRejectedValue({ code: 'server_error', message: 'boom' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.errors.server_error)
    expect(w.text()).not.toContain(en.admin.upstream.notFound)
  })
  it('does not navigate when delete fails', async () => {
    get.mockResolvedValue(IDP); post.mockRejectedValue({ code: 'server_error', message: 'boom' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    await clickConfirm(w, en.admin.upstream.delete)
    expect(push).not.toHaveBeenCalled()
  })
})
