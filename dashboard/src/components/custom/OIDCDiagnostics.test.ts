import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import OIDCDiagnostics from './OIDCDiagnostics.vue'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get), post = vi.mocked(api.post)
const effective = { mode: 'discovery', fetchedAt: '2026-09-17T00:00:00Z', callbackUrl: 'https://idp.example/api/prohibitorum/auth/federation/corp/test/callback', fields: { tokenEndpoint: { value: 'https://upstream.example/token', source: 'override' } } }
const result = { status: 'succeeded', expiresAt: '2026-09-17T00:10:00Z', stages: [{ name: 'token_exchange', status: 'succeeded', httpStatus: 200 }, { name: 'id_token', status: 'succeeded' }], claims: { subject: 'upstream-user' } }
const id = 'A'.repeat(43)
function view(dirty = false) { return mount(OIDCDiagnostics, { props: { slug: 'corp', dirty }, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })] } }) }
beforeEach(() => { get.mockReset(); post.mockReset(); window.history.replaceState({}, '', '/admin/identity-providers/corp') })
afterEach(() => { vi.useRealTimers() })
describe('OIDCDiagnostics', () => {
  it('requires an explicit fresh read before testing and blocks unsaved drafts', async () => {
    get.mockResolvedValue(effective)
    const w = view(); expect(w.get('[data-test="oidc-test-start"]').attributes('disabled')).toBeDefined()
    await w.get('[data-test="effective-refresh"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain('https://upstream.example/token')
    expect(w.get('[data-test="test-callback"]').text()).toBe(effective.callbackUrl)
    expect(w.get('[data-test="oidc-test-start"]').attributes('disabled')).toBeUndefined()
    await w.setProps({ dirty: true }); expect(w.text()).toContain(en.admin.upstream.diagnostics.saveFirst)
    expect(w.get('[data-test="oidc-test-start"]').attributes('disabled')).toBeDefined(); w.unmount()
  })
  it('keeps previous values marked stale after a failed discovery read', async () => {
    get.mockResolvedValueOnce(effective).mockRejectedValueOnce({ code: 'upstream_temporarily_unavailable', details: { stage: 'discovery', endpoint: 'https://upstream.example', durationMs: 5, errorCode: 'discovery_failed' } })
    const w = view(); await w.get('[data-test="effective-refresh"]').trigger('click'); await flushPromises()
    await w.get('[data-test="effective-refresh"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain(effective.fields.tokenEndpoint.value); expect(w.text()).toContain(en.admin.upstream.diagnostics.stale)
    expect(w.get('[data-test="discovery-error"]').text()).toContain(en.admin.upstream.diagnostics.errors.discovery_failed); w.unmount()
  })
  it('completes a ready callback once and restores completed results without exchange on refresh', async () => {
    window.history.replaceState({}, '', '/admin/identity-providers/corp?test=' + id)
    get.mockResolvedValueOnce({ ...result, status: 'ready' }); post.mockResolvedValue(result)
    const first = view(); await flushPromises()
    expect(post).toHaveBeenCalledTimes(1); expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/corp/tests/' + id + '/complete')
    expect(first.text()).toContain('upstream-user'); first.unmount()
    get.mockResolvedValue(result)
    const second = view(); await flushPromises(); expect(post).toHaveBeenCalledTimes(1)
    expect(second.get('[data-test="test-status"]').text()).toBe('Succeeded'); second.unmount()
  })
  it('polls running results without replaying complete and stops on unmount', async () => {
    vi.useFakeTimers(); window.history.replaceState({}, '', '/admin/identity-providers/corp?test=' + id)
    get.mockResolvedValue({ ...result, status: 'running' })
    const w = view(); await flushPromises(); await vi.advanceTimersByTimeAsync(1000); await flushPromises()
    expect(get).toHaveBeenCalledTimes(2); expect(post).not.toHaveBeenCalled(); w.unmount()
    await vi.advanceTimersByTimeAsync(5000); expect(get).toHaveBeenCalledTimes(2)
  })
  it('ignores malformed test ids', async () => {
    window.history.replaceState({}, '', '/admin/identity-providers/corp?test=bad/id')
    const w = view(); await flushPromises(); expect(get).not.toHaveBeenCalled(); expect(post).not.toHaveBeenCalled(); w.unmount()
  })
  it('keeps results closed when an in-flight poll finishes later', async () => {
    vi.useFakeTimers(); window.history.replaceState({}, '', '/admin/identity-providers/corp?test=' + id)
    let finish!: (value: unknown) => void
    get.mockResolvedValueOnce({ ...result, status: 'running' }).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const w = view(); await flushPromises(); await vi.advanceTimersByTimeAsync(1000)
    await w.findAll('button').find(button => button.text() === 'Close')!.trigger('click')
    finish(result); await flushPromises(); await vi.advanceTimersByTimeAsync(5000)
    expect(w.find('[data-test="test-status"]').exists()).toBe(false)
    expect(window.location.search).toBe(''); expect(get).toHaveBeenCalledTimes(2); w.unmount()
  })
})
