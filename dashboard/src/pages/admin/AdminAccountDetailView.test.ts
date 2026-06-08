import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post); const put = vi.mocked(api.put)
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }), useRoute: () => ({ params: { id: '7' } }) }))
import AdminAccountDetailView from './AdminAccountDetailView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminAccountDetailView, {
  global: { plugins: [i18n()], stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } } },
  attachTo: document.body,
})
const ACCOUNT = {
  id: 7, username: 'carol', displayName: 'Carol Ng', role: 'user',
  attributes: { team: 'security' }, disabled: false,
  createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-02-01T00:00:00Z', lastSignInAt: '2026-06-01T00:00:00Z',
}
const CREDS = [
  { id: 11, credentialIdSuffix: 'ab12', nickname: 'Laptop', transports: ['internal'], backupState: true, attestationType: 'none', createdAt: '2026-01-02T00:00:00Z', lastUsedAt: '2026-06-01T00:00:00Z' },
]
const SESSIONS = [
  { id: 'sess-aaa', isCurrent: true,  issuedAt: '2026-06-01T10:00:00Z', expiresAt: '2026-06-08T10:00:00Z', lastSeenIp: '1.2.3.4', userAgent: 'Firefox/126' },
  { id: 'sess-bbb', isCurrent: false, issuedAt: '2026-06-02T10:00:00Z', expiresAt: '2026-06-09T10:00:00Z', lastSeenIp: '5.6.7.8', userAgent: 'Chrome/125' },
]
// GET router: /accounts/7 → account; /accounts/7/credentials → creds; /accounts/7/sessions → sessions
function mockGets(account = ACCOUNT, creds = CREDS, sess = SESSIONS) {
  get.mockImplementation(async (p: string) => {
    if (p.endsWith('/credentials')) return creds
    if (p.endsWith('/sessions')) return sess
    return account
  })
}
// ConfirmDialog confirm = destructive button (teleported to body) with the given label.
function clickConfirm(label: string) {
  const btns = Array.from(document.body.querySelectorAll('button'))
    .filter((b) => b.getAttribute('data-variant') === 'destructive' && b.textContent?.includes(label))
  btns[btns.length - 1]!.click()
}
beforeEach(() => { get.mockReset(); post.mockReset(); put.mockReset(); push.mockReset() })

describe('AdminAccountDetailView', () => {
  it('loads the account and its credentials', async () => {
    mockGets()
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/accounts/7')
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/accounts/7/credentials')
    expect(w.text()).toContain('Carol Ng')
    expect(w.text()).toContain('Laptop')
    expect(w.text()).toContain('team')
  })
  it('shows not-found when the account is missing', async () => {
    get.mockRejectedValue({ code: 'account_not_found', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.account.notFound)
  })
  it('shows the error banner when the initial load fails (non-404)', async () => {
    get.mockRejectedValue({ code: 'forbidden', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.errors.forbidden)
  })
  it('saves identity, round-tripping existing attributes', async () => {
    mockGets()
    put.mockResolvedValue({ ...ACCOUNT, role: 'admin' })
    const w = mountView(); await flushPromises()
    await w.find<HTMLSelectElement>('select[name="role"]').setValue('admin')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/accounts/7', {
      username: '', displayName: 'Carol Ng', role: 'admin', disabled: false, attributes: { team: 'security' },
    })
    expect(w.text()).toContain(en.admin.account.saved)
  })
  it('surfaces last_admin on save failure', async () => {
    mockGets()
    put.mockRejectedValue({ code: 'last_admin', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.errors.last_admin)
  })
  it('force-revokes a passkey (confirm → post → refresh)', async () => {
    mockGets(); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="revoke-cred-11"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.account.forceRevoke); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/accounts/credentials/delete', { accountId: 7, credentialId: 11 })
    expect(get.mock.calls.filter((c) => String(c[0]).endsWith('/credentials')).length).toBe(2)
  })
  it('revokes all sessions and shows the count', async () => {
    mockGets(); post.mockResolvedValue({ revoked: 3 })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="revoke-all"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.account.revokeAllSessions); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/accounts/revoke-sessions', { id: 7 })
    expect(w.text()).toContain('Revoked 3')
  })
  it('reissues an enrollment link and reveals the URL', async () => {
    mockGets(); post.mockResolvedValue({ url: 'https://x/enroll/tok', expiresAt: '2026-06-09T00:00:00Z' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="reissue"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/accounts/reissue-enrollment', { id: 7 })
    expect(w.text()).toContain('https://x/enroll/tok')
  })
  it('deletes the account and navigates to the list', async () => {
    mockGets(); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.account.delete); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/accounts/delete', { id: 7 })
    expect(push).toHaveBeenCalledWith('/admin/accounts')
  })
  it('does not navigate when delete fails', async () => {
    mockGets()
    post.mockRejectedValue({ code: 'cannot_delete_self', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.account.delete); await flushPromises()
    expect(push).not.toHaveBeenCalled()
    expect(w.text()).toContain(en.errors.cannot_delete_self)
  })
  it('loads and renders session rows on mount', async () => {
    mockGets()
    const w = mountView(); await flushPromises()
    expect(get.mock.calls.some((c) => String(c[0]).endsWith('/sessions'))).toBe(true)
    expect(w.find('[data-test="session-row-sess-aaa"]').exists()).toBe(true)
    expect(w.find('[data-test="session-row-sess-bbb"]').exists()).toBe(true)
    expect(w.find('[data-test="session-revoke-sess-aaa"]').exists()).toBe(true)
    expect(w.find('[data-test="session-revoke-sess-bbb"]').exists()).toBe(true)
  })
  it('per-row revoke posts with sessionId and re-fetches the session list', async () => {
    mockGets(); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    const getSessionsBefore = get.mock.calls.filter((c) => String(c[0]).endsWith('/sessions')).length
    await w.find('[data-test="session-revoke-sess-bbb"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/accounts/7/sessions/revoke', { sessionId: 'sess-bbb' })
    const getSessionsAfter = get.mock.calls.filter((c) => String(c[0]).endsWith('/sessions')).length
    expect(getSessionsAfter).toBe(getSessionsBefore + 1)
  })
  it('shows session_not_found error when per-row revoke fails', async () => {
    mockGets()
    post.mockRejectedValue({ code: 'session_not_found', message: 'some-zh-text' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="session-revoke-sess-bbb"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.errors.session_not_found)
  })
  it('shows empty state when sessions list is empty', async () => {
    mockGets(ACCOUNT, CREDS, [])
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.account.sessions.empty)
  })
  it('revoke-all re-fetches the session list', async () => {
    mockGets(); post.mockResolvedValue({ revoked: 2 })
    const w = mountView(); await flushPromises()
    const getSessionsBefore = get.mock.calls.filter((c) => String(c[0]).endsWith('/sessions')).length
    await w.find('[data-test="revoke-all"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.account.revokeAllSessions); await flushPromises()
    const getSessionsAfter = get.mock.calls.filter((c) => String(c[0]).endsWith('/sessions')).length
    expect(getSessionsAfter).toBe(getSessionsBefore + 1)
  })
})
