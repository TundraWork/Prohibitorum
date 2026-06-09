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
  attributes: { team: 'security', score: 42 }, disabled: false,
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
    // string attr 'team' is seeded into the editable row editor
    expect(w.find('[data-test="attr-row-0"]').exists()).toBe(true)
    expect(w.find<HTMLInputElement>('[data-test="attr-key-0"]').element.value).toBe('team')
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
    await w.find('[data-test="segment-admin"]').trigger('click'); await flushPromises()
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/accounts/7', {
      username: '', displayName: 'Carol Ng', role: 'admin', disabled: false,
      attributes: { team: 'security', score: 42 },
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

  // ---- attributes editor ----

  it('edits an existing string attr value + adds a new row → PUT body attributes reflect edits', async () => {
    mockGets()
    put.mockResolvedValue({ ...ACCOUNT })
    const w = mountView(); await flushPromises()
    // edit existing string attr 'team' value from 'security' to 'engineering'
    const valInput = w.find<HTMLInputElement>('[data-test="attr-value-0"]')
    await valInput.setValue('engineering')
    // add a new row and fill it in
    await w.find('[data-test="attr-add"]').trigger('click'); await flushPromises()
    const keyInput1 = w.find<HTMLInputElement>('[data-test="attr-key-1"]')
    const valInput1 = w.find<HTMLInputElement>('[data-test="attr-value-1"]')
    await keyInput1.setValue('region')
    await valInput1.setValue('eu-west')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/accounts/7', expect.objectContaining({
      attributes: { team: 'engineering', region: 'eu-west', score: 42 },
    }))
  })

  it('remove a row → that key is dropped from PUT attributes', async () => {
    mockGets()
    put.mockResolvedValue({ ...ACCOUNT })
    const w = mountView(); await flushPromises()
    // remove the only string attr row (team)
    await w.find('[data-test="attr-remove-0"]').trigger('click'); await flushPromises()
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    const call = put.mock.calls[0]!
    const body = call[1] as { attributes: Record<string, unknown> }
    expect(body.attributes).not.toHaveProperty('team')
    // non-string 'score' still preserved
    expect(body.attributes).toMatchObject({ score: 42 })
  })

  it('non-string attribute is preserved in PUT and not turned into an editable row', async () => {
    mockGets()
    put.mockResolvedValue({ ...ACCOUNT })
    const w = mountView(); await flushPromises()
    // score=42 (number) must NOT produce an editable row — only team=string is row 0
    expect(w.find('[data-test="attr-row-0"]').exists()).toBe(true)
    expect(w.find('[data-test="attr-row-1"]').exists()).toBe(false) // no second editable row
    // save without touching anything → exact PUT attributes: team + score preserved
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/accounts/7', expect.objectContaining({
      attributes: { team: 'security', score: 42 },
    }))
    const call = put.mock.calls[0]!
    const body = call[1] as { attributes: Record<string, unknown> }
    expect(typeof body.attributes.score).toBe('number')
  })

  it('complex key collision: editable row whose key matches a complex key does NOT overwrite the complex value', async () => {
    mockGets()
    put.mockResolvedValue({ ...ACCOUNT })
    const w = mountView(); await flushPromises()
    // add a new editable row and type the complex key 'score' with a string value
    await w.find('[data-test="attr-add"]').trigger('click'); await flushPromises()
    const keyInput1 = w.find<HTMLInputElement>('[data-test="attr-key-1"]')
    const valInput1 = w.find<HTMLInputElement>('[data-test="attr-value-1"]')
    await keyInput1.setValue('score')
    await valInput1.setValue('not-a-number')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    const call = put.mock.calls[0]!
    const body = call[1] as { attributes: Record<string, unknown> }
    // complex value must be preserved — string row must NOT win
    expect(body.attributes.score).toBe(42)
    expect(typeof body.attributes.score).toBe('number')
    // existing string attr and the preserved complex are both present
    expect(body.attributes).toEqual({ team: 'security', score: 42 })
  })

  it('empty-key rows are skipped on save', async () => {
    mockGets()
    put.mockResolvedValue({ ...ACCOUNT })
    const w = mountView(); await flushPromises()
    // add a row but leave key empty
    await w.find('[data-test="attr-add"]').trigger('click'); await flushPromises()
    const valInput1 = w.find<HTMLInputElement>('[data-test="attr-value-1"]')
    await valInput1.setValue('orphan')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    const call = put.mock.calls[0]!
    const body = call[1] as { attributes: Record<string, unknown> }
    // the empty-key row must not appear in attributes
    expect(Object.keys(body.attributes)).not.toContain('')
    expect(body.attributes).toMatchObject({ team: 'security', score: 42 })
  })
})
