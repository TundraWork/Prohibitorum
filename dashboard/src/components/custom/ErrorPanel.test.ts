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
    const w = mount(ErrorPanel, {
      props: { error: { code: 'rate_limited', requestId: 'rid' } },
      global: { plugins: [makeI18n()] },
    })
    const btn = w.find('[data-test="error-recovery"]')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    expect(w.emitted('recovery')).toBeTruthy()
  })

  it('shows a reauth recovery for sudo_required', async () => {
    const w = mount(ErrorPanel, {
      props: { error: { code: 'sudo_required', requestId: 'rid' } },
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

describe('ErrorPanel — admin diagnostic action', () => {
  beforeEach(() => { vi.useFakeTimers() })
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

  it('emits diagnostic when the admin diagnostic button is clicked', async () => {
    const w = mount(ErrorPanel, {
      props: { error: KNOWN_ERROR, isAdmin: true },
      global: { plugins: [makeI18n()] },
    })
    await w.get('[data-test="error-diagnostic"]').trigger('click')
    expect(w.emitted('diagnostic')).toBeTruthy()
    expect(w.emitted('diagnostic')![0]).toEqual([{ requestId: 'rid-123' }])
  })

  it('does not show the diagnostic action without a requestId', () => {
    const w = mount(ErrorPanel, {
      props: { error: { code: 'bad_request' }, isAdmin: true },
      global: { plugins: [makeI18n()] },
    })
    expect(w.find('[data-test="error-diagnostic"]').exists()).toBe(false)
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
