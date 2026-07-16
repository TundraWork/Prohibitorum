import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import ErrorPanel from './ErrorPanel.vue'
import en from '@/locales/en'
import zh from '@/locales/zh'
import type { ApiError } from '@/lib/errors'

function makeI18n(locale: 'en' | 'zh' = 'en') {
  return createI18n({
    legacy: false,
    locale,
    fallbackLocale: 'en',
    messages: { en, zh },
  })
}

const KNOWN_ERROR: ApiError = {
  code: 'account_disabled',
  requestId: 'rid-123',
}

const UNKNOWN_ERROR: ApiError = {
  code: 'some_unknown_code_xyz',
  requestId: 'rid-456',
}

const DETAIL_ERROR: ApiError = {
  code: 'invalid_role',
  details: { allowed: ['user', 'admin'] },
  requestId: 'rid-789',
}

const RATE_LIMIT_ERROR: ApiError = {
  code: 'rate_limited',
  details: { retryAfterSeconds: 30 },
  requestId: 'rid-rl',
}

describe('ErrorPanel — rendering', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('renders the localized message for a known code', () => {
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR },
      global: { plugins: [makeI18n()] },
    })
    expect(w.text()).toContain(en.errors.codes.account_disabled)
  })

  it('renders the unknown fallback for an unregistered code', () => {
    const w = mount(ErrorPanel, {
      props: { error: UNKNOWN_ERROR },
      global: { plugins: [makeI18n()] },
    })
    expect(w.text()).toContain(en.errors.unknown)
  })

  it('has role=alert so assistive tech announces it', () => {
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR },
      global: { plugins: [makeI18n()] },
    })
    expect(w.find('[role="alert"]').exists()).toBe(true)
  })

  it('does NOT display the raw requestId in the summary', () => {
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR },
      global: { plugins: [makeI18n()] },
    })
    expect(w.text()).not.toContain('rid-123')
  })
})

describe('ErrorPanel — persistence', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('persists after 60 seconds (no auto-dismiss)', () => {
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR },
      global: { plugins: [makeI18n()] },
    })
    vi.advanceTimersByTime(60_000)
    expect(w.find('[role="alert"]').exists()).toBe(true)
    expect(w.text()).toContain(en.errors.codes.account_disabled)
  })

  it('emits dismiss when the close button is clicked', async () => {
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR },
      global: { plugins: [makeI18n()] },
    })
    await w.get('[data-test="error-dismiss"]').trigger('click')
    expect(w.emitted('dismiss')).toBeTruthy()
  })

  it('can keep a terminal error mounted without a dismiss action', () => {
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR, dismissible: false },
      global: { plugins: [makeI18n()] },
    })

    expect(w.find('[data-test="error-dismiss"]').exists()).toBe(false)
    expect(w.get('[role="alert"]').text()).toContain(en.errors.codes.account_disabled)
  })

  it('gives every error-panel control a 44px hit target', async () => {
    const w = mount(ErrorPanel, {
      props: { error: RATE_LIMIT_ERROR, isAdmin: true },
      attrs: { onRecovery: vi.fn() },
      global: { plugins: [makeI18n()] },
    })

    expect(w.get('[data-test="error-dismiss"]').classes()).toEqual(expect.arrayContaining(['min-h-11', 'min-w-11']))
    expect(w.get('[data-test="error-recovery"]').classes()).toContain('min-h-11')
    expect(w.get('[data-test="error-details-trigger"]').classes()).toContain('min-h-11')
    expect(w.get('[data-test="error-diagnostic"]').classes()).toContain('min-h-11')

    await w.get('[data-test="error-details-trigger"]').trigger('click')
    await nextTick()
    expect(w.get('[data-test="error-copy-request-id"]').classes()).toEqual(expect.arrayContaining(['min-h-11', 'min-w-11']))
  })
})

describe('ErrorPanel — details disclosure', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('hides details until the trigger is clicked', async () => {
    const w = mount(ErrorPanel, {
      props: { error: DETAIL_ERROR },
      global: { plugins: [makeI18n()] },
    })
    // requestId is hidden initially
    expect(w.text()).not.toContain('rid-789')
    await w.get('[data-test="error-details-trigger"]').trigger('click')
    await nextTick()
    // After expand, requestId is shown
    expect(w.text()).toContain('rid-789')
  })

  it('shows localized detail labels and values in the disclosure', async () => {
    const w = mount(ErrorPanel, {
      props: { error: DETAIL_ERROR },
      global: { plugins: [makeI18n()] },
    })
    await w.get('[data-test="error-details-trigger"]').trigger('click')
    await nextTick()
    expect(w.text()).toContain(en.errors.details.allowed)
    expect(w.text()).toContain('user')
    expect(w.text()).toContain('admin')
  })

  it('is keyboard accessible — trigger has aria-expanded', async () => {
    const w = mount(ErrorPanel, {
      props: { error: DETAIL_ERROR },
      global: { plugins: [makeI18n()] },
    })
    const trigger = w.get('[data-test="error-details-trigger"]')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    await trigger.trigger('click')
    await nextTick()
    expect(trigger.attributes('aria-expanded')).toBe('true')
  })

  it('shows retryAfterSeconds in the details', async () => {
    const w = mount(ErrorPanel, {
      props: { error: RATE_LIMIT_ERROR },
      global: { plugins: [makeI18n()] },
    })
    await w.get('[data-test="error-details-trigger"]').trigger('click')
    await nextTick()
    expect(w.text()).toContain('30')
    expect(w.text()).toContain(en.errors.details.retryAfterSeconds)
  })
})

describe('ErrorPanel — copy request ID', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('has a copy-request-id button that copies the requestId', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR },
      global: { plugins: [makeI18n()] },
    })
    await w.get('[data-test="error-details-trigger"]').trigger('click')
    await nextTick()
    await w.get('[data-test="error-copy-request-id"]').trigger('click')
    expect(writeText).toHaveBeenCalledWith('rid-123')
    vi.unstubAllGlobals()
  })

  it('does not show the copy button when requestId is absent', () => {
    const w = mount(ErrorPanel, {
      props: { error: { code: 'bad_request' } },
      global: { plugins: [makeI18n()] },
    })
    expect(w.find('[data-test="error-copy-request-id"]').exists()).toBe(false)
  })
})

describe('ErrorPanel — recovery action', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('emits recovery when the recovery button is clicked', async () => {
    const onRecovery = vi.fn()
    const w = mount(ErrorPanel, {
      props: { error: { code: 'rate_limited', requestId: 'rid' } },
      attrs: { onRecovery },
      global: { plugins: [makeI18n()] },
    })
    const btn = w.find('[data-test="error-recovery"]')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    expect(onRecovery).toHaveBeenCalled()
  })

  it('shows a reauth recovery for sudo_required', async () => {
    const onRecovery = vi.fn()
    const w = mount(ErrorPanel, {
      props: { error: { code: 'sudo_required', requestId: 'rid' } },
      attrs: { onRecovery },
      global: { plugins: [makeI18n()] },
    })
    expect(w.get('[data-test="error-recovery"]').text()).toContain(en.errors.recovery.reauth)
  })


  it('does not show recovery button when no recovery hint', () => {
    const w = mount(ErrorPanel, {
      props: { error: { code: 'bad_request', requestId: 'rid' } },
      global: { plugins: [makeI18n()] },
    })
    expect(w.find('[data-test="error-recovery"]').exists()).toBe(false)
  })
})
// Mock api for diagnostic fetch tests
vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), del: vi.fn(), upload: vi.fn() },
}))
import { api } from '@/lib/api'

const DIAGNOSTIC_RECORD = {
  requestId: 'rid-123',
  code: 'account_disabled',
  operation: 'admin.update_account',
  method: 'PUT',
  route: '/api/prohibitorum/accounts/42',
  retryable: false,
  fields: { status: 'disabled' },
  occurredAt: '2025-01-01T00:00:00Z',
  expiresAt: '2025-01-08T00:00:00Z',
}

describe('ErrorPanel — admin diagnostic action', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.mocked(api.get).mockReset() })
  afterEach(() => { vi.useRealTimers() })

  it('shows the diagnostic action for admin users', () => {
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR, isAdmin: true },
      global: { plugins: [makeI18n()] },
    })
    expect(w.find('[data-test="error-diagnostic"]').exists()).toBe(true)
  })

  it('hides the diagnostic action for non-admin users', () => {
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR, isAdmin: false },
      global: { plugins: [makeI18n()] },
    })
    expect(w.find('[data-test="error-diagnostic"]').exists()).toBe(false)
  })

  it('does not show the diagnostic action without a requestId', () => {
    const w = mount(ErrorPanel, {
      props: { error: { code: 'bad_request' }, isAdmin: true },
      global: { plugins: [makeI18n()] },
    })
    expect(w.find('[data-test="error-diagnostic"]').exists()).toBe(false)
  })

  it('fetches the diagnostic record when the button is clicked and renders it', async () => {
    vi.mocked(api.get).mockResolvedValue(DIAGNOSTIC_RECORD)
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR, isAdmin: true },
      global: { plugins: [makeI18n()] },
    })
    await w.get('[data-test="error-diagnostic"]').trigger('click')
    await flushPromises()
    expect(api.get).toHaveBeenCalledWith('/api/prohibitorum/diagnostics/rid-123')
    // The diagnostic record is rendered persistently
    expect(w.find('[data-test="diagnostic-record"]').exists()).toBe(true)
    expect(w.text()).toContain('admin.update_account')
    expect(w.text()).toContain('account_disabled')
  })

  it('shows a loading state while fetching the diagnostic record', async () => {
    let resolveFn!: (v: typeof DIAGNOSTIC_RECORD) => void
    vi.mocked(api.get).mockReturnValue(new Promise((r) => { resolveFn = r }))
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR, isAdmin: true },
      global: { plugins: [makeI18n()] },
    })
    await w.get('[data-test="error-diagnostic"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="diagnostic-loading"]').exists()).toBe(true)
    resolveFn(DIAGNOSTIC_RECORD)
    await flushPromises()
    expect(w.find('[data-test="diagnostic-loading"]').exists()).toBe(false)
  })

  it('shows an error state when the diagnostic lookup fails', async () => {
    vi.mocked(api.get).mockRejectedValue({ code: 'account_not_found' })
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR, isAdmin: true },
      global: { plugins: [makeI18n()] },
    })
    await w.get('[data-test="error-diagnostic"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="diagnostic-error"]').exists()).toBe(true)
    // The diagnostic error should be localized, not raw server prose
    expect(w.text()).not.toContain('account_not_found')
  })

  it('renders the diagnostic record in an accessible region', async () => {
    vi.mocked(api.get).mockResolvedValue(DIAGNOSTIC_RECORD)
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR, isAdmin: true },
      global: { plugins: [makeI18n()] },
    })
    await w.get('[data-test="error-diagnostic"]').trigger('click')
    await flushPromises()
    const record = w.find('[data-test="diagnostic-record"]')
    expect(record.exists()).toBe(true)
    // The record region should be labeled for screen readers
    expect(record.attributes('role')).toBeDefined()
  })

  it('persists the diagnostic record after 60 seconds (no auto-dismiss)', async () => {
    vi.mocked(api.get).mockResolvedValue(DIAGNOSTIC_RECORD)
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR, isAdmin: true },
      global: { plugins: [makeI18n()] },
    })
    await w.get('[data-test="error-diagnostic"]').trigger('click')
    await flushPromises()
    vi.advanceTimersByTime(60_000)
    expect(w.find('[data-test="diagnostic-record"]').exists()).toBe(true)
  })
})
describe('ErrorPanel — diagnostic fetch staleness guard', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.mocked(api.get).mockReset() })
  afterEach(() => { vi.useRealTimers() })

  it('does not write stale state when the error changes during fetch', async () => {
    let resolveFirst!: (v: typeof DIAGNOSTIC_RECORD) => void
    vi.mocked(api.get).mockReturnValueOnce(new Promise((r) => { resolveFirst = r }))
    const w = mount(ErrorPanel, {
      props: { error: { code: 'forbidden', requestId: 'rid-first' }, isAdmin: true },
      global: { plugins: [makeI18n()] },
    })
    // Start fetch for rid-first
    await w.get('[data-test="error-diagnostic"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="diagnostic-loading"]').exists()).toBe(true)

    // Error changes to a different requestId while the first fetch is in-flight
    await w.setProps({ error: { code: 'bad_request', requestId: 'rid-second' } })
    await flushPromises()

    // The first fetch resolves — but its result must NOT be written to state
    resolveFirst(DIAGNOSTIC_RECORD)
    await flushPromises()

    // No diagnostic record should be shown — the requestId changed
    expect(w.find('[data-test="diagnostic-record"]').exists()).toBe(false)
    // Loading state should be cleared (stale fetch was discarded)
    expect(w.find('[data-test="diagnostic-loading"]').exists()).toBe(false)
  })

  it('does not write stale state when dismissed during fetch', async () => {
    let resolveFetch!: (v: typeof DIAGNOSTIC_RECORD) => void
    vi.mocked(api.get).mockReturnValueOnce(new Promise((r) => { resolveFetch = r }))
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR, isAdmin: true },
      global: { plugins: [makeI18n()] },
    })
    await w.get('[data-test="error-diagnostic"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="diagnostic-loading"]').exists()).toBe(true)

    // Dismiss the error (parent clears error → ErrorPanel unmounts)
    await w.setProps({ error: null })
    await flushPromises()

    // The pending fetch resolves — must NOT crash or write state
    resolveFetch(DIAGNOSTIC_RECORD)
    await flushPromises()

    // ErrorPanel is gone
    expect(w.find('[role="alert"]').exists()).toBe(false)
  })

  it('does not write state when unmounted during in-flight diagnostic fetch', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    let resolveFetch!: (v: typeof DIAGNOSTIC_RECORD) => void
    vi.mocked(api.get).mockReturnValueOnce(new Promise((r) => { resolveFetch = r }))
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR, isAdmin: true },
      global: { plugins: [makeI18n()] },
    })
    await w.get('[data-test="error-diagnostic"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="diagnostic-loading"]').exists()).toBe(true)

    // Unmount the component entirely while the fetch is in-flight
    w.unmount()

    // The pending fetch resolves — must NOT throw, warn, or write state
    resolveFetch(DIAGNOSTIC_RECORD)
    await flushPromises()

    // No Vue warning about writing to unmounted component
    const vueWarnings = warnSpy.mock.calls
      .map((c) => String(c))
      .filter((s) => s.includes('unmounted') || s.includes('unmount'))
    expect(vueWarnings).toEqual([])
    warnSpy.mockRestore()
  })
})

describe('ErrorPanel — recovery button (M6)', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('shows recovery guidance text but NOT a button when no @recovery listener is attached', () => {
    const w = mount(ErrorPanel, {
      props: { error: { code: 'rate_limited', requestId: 'rid' } },
      global: { plugins: [makeI18n()] },
    })
    // Recovery guidance text is present
    expect(w.text()).toContain(en.errors.recovery.retry)
    // But NO action button (no @recovery listener wired)
    expect(w.find('[data-test="error-recovery"]').exists()).toBe(false)
  })

  it('shows a recovery action button when an @recovery listener is attached', () => {
    const onRecovery = vi.fn()
    const w = mount(ErrorPanel, {
      props: { error: { code: 'rate_limited', requestId: 'rid' } },
      attrs: { onRecovery },
      global: { plugins: [makeI18n()] },
    })
    expect(w.find('[data-test="error-recovery"]').exists()).toBe(true)
  })

  it('clicking the recovery button emits recovery when listener is attached', async () => {
    const onRecovery = vi.fn()
    const w = mount(ErrorPanel, {
      props: { error: { code: 'rate_limited', requestId: 'rid' } },
      attrs: { onRecovery },
      global: { plugins: [makeI18n()] },
    })
    await w.get('[data-test="error-recovery"]').trigger('click')
    expect(onRecovery).toHaveBeenCalled()
  })
})

describe('ErrorPanel — locale parity (zh)', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('renders the localized zh message for a known code', () => {
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR },
      global: { plugins: [makeI18n('zh')] },
    })
    expect(w.text()).toContain(zh.errors.codes.account_disabled)
  })

  it('renders the zh unknown fallback for an unregistered code', () => {
    const w = mount(ErrorPanel, {
      props: { error: UNKNOWN_ERROR },
      global: { plugins: [makeI18n('zh')] },
    })
    expect(w.text()).toContain(zh.errors.unknown)
  })
})
