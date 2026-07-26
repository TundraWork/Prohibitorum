import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import type { Rule, RulePreviewPage } from '@/lib/appAccess'

vi.mock('@/lib/api', () => ({ api: { post: vi.fn() } }))

import { api } from '@/lib/api'
import RuleImpactPreview from './RuleImpactPreview.vue'

const post = vi.mocked(api.post)
const PROVIDERS = [{ slug: 'corporate', displayName: 'Corporate identity' }]
const PASSKEY_RULE: Rule = {
  version: 1,
  condition: { fact: 'login_method', method: 'passkey' },
}
const FEDERATION_RULE: Rule = {
  version: 1,
  condition: { fact: 'login_method', method: 'federation' },
}
const INCOMPLETE_RULE: Rule = {
  version: 1,
  condition: { op: 'all', children: [{}] },
}
const PAGE_ONE: RulePreviewPage = {
  matchedCount: 3,
  items: [{ account: { id: 7, username: 'alice', displayName: 'Alice Ng' }, matched: true }],
  nextCursor: 'next:alice',
}
const PAGE_TWO: RulePreviewPage = {
  matchedCount: 3,
  items: [{ account: { id: 42, username: 'bob', displayName: 'Bob Ruiz' }, matched: false }],
  nextCursor: '',
}

const wrappers: VueWrapper[] = []
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function mountPreview(rule: Rule) {
  const wrapper = mount(RuleImpactPreview, {
    props: {
      rule,
      providers: PROVIDERS,
      endpoint: '/managed-applications/oidc/wiki/rule-preview',
      limit: 1,
    },
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })
  wrappers.push(wrapper)
  return wrapper
}

beforeEach(() => {
  vi.useFakeTimers()
  post.mockReset()
})

afterEach(() => {
  while (wrappers.length) wrappers.pop()!.unmount()
  document.body.innerHTML = ''
  vi.useRealTimers()
})

describe('RuleImpactPreview', () => {
  it('debounces valid drafts by 300 ms and sends the closed rule with cursor and limit', async () => {
    const wrapper = mountPreview(INCOMPLETE_RULE)

    await vi.advanceTimersByTimeAsync(1_000)
    expect(post).not.toHaveBeenCalled()

    post.mockResolvedValue(PAGE_ONE)
    await wrapper.setProps({ rule: PASSKEY_RULE })
    await vi.advanceTimersByTimeAsync(299)
    expect(post).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()

    expect(post).toHaveBeenCalledWith('/managed-applications/oidc/wiki/rule-preview', {
      version: 1,
      condition: { fact: 'login_method', method: 'passkey' },
      cursor: '',
      limit: 1,
    })
  })

  it('ignores a superseded response and then previews the latest draft', async () => {
    const first = deferred<RulePreviewPage>()
    const second = deferred<RulePreviewPage>()
    post.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const wrapper = mountPreview(PASSKEY_RULE)

    await vi.advanceTimersByTimeAsync(300)
    await wrapper.setProps({ rule: FEDERATION_RULE })
    await vi.advanceTimersByTimeAsync(300)

    first.resolve(PAGE_ONE)
    await flushPromises()
    expect(wrapper.find('[data-test="impact-row-7"]').exists()).toBe(false)
    expect(post).toHaveBeenCalledTimes(2)
    expect(post.mock.calls[1]?.[1]).toEqual({
      version: 1,
      condition: { fact: 'login_method', method: 'federation' },
      cursor: '',
      limit: 1,
    })

    second.resolve({ ...PAGE_TWO, matchedCount: 1 })
    await flushPromises()
    expect(wrapper.get('[data-test="impact-count"]').text()).toContain('1')
    expect(wrapper.get('[data-test="impact-row-42"]').text()).toContain('Bob Ruiz')
  })

  it('stops showing loading when the latest request resolves even if a superseded request remains pending', async () => {
    const superseded = deferred<RulePreviewPage>()
    const latest = deferred<RulePreviewPage>()
    post.mockReturnValueOnce(superseded.promise).mockReturnValueOnce(latest.promise)
    const wrapper = mountPreview(PASSKEY_RULE)

    await vi.advanceTimersByTimeAsync(300)
    await wrapper.setProps({ rule: FEDERATION_RULE })
    await vi.advanceTimersByTimeAsync(300)

    latest.resolve(PAGE_ONE)
    await flushPromises()
    expect(wrapper.get('[data-test="impact-status"]').text()).toBe('Impact preview is current.')
    expect(wrapper.get('[data-test="next-page"]').attributes('disabled')).toBeUndefined()

    superseded.resolve(PAGE_TWO)
    await flushPromises()
    expect(wrapper.get('[data-test="impact-status"]').text()).toBe('Impact preview is current.')
    expect(wrapper.get('[data-test="impact-row-7"]').exists()).toBe(true)
  })

  it('renders the exact match count and safe account page, then paginates with the returned cursor', async () => {
    post.mockResolvedValueOnce(PAGE_ONE).mockResolvedValueOnce(PAGE_TWO)
    const wrapper = mountPreview(PASSKEY_RULE)

    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(wrapper.get('[data-test="impact-count"]').text()).toBe('3 active accounts match')
    expect(wrapper.get('[data-test="impact-row-7"]').text()).toContain('Alice Ng')
    expect(wrapper.get('[data-test="impact-row-7"]').text()).toContain('alice')
    expect(wrapper.get('[data-test="impact-row-7"]').text()).toContain('Matches')

    await wrapper.get('[data-test="next-page"]').trigger('click')
    await flushPromises()
    expect(post.mock.calls[1]?.[1]).toEqual({
      version: 1,
      condition: { fact: 'login_method', method: 'passkey' },
      cursor: 'next:alice',
      limit: 1,
    })
    expect(wrapper.find('[data-test="impact-row-7"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="impact-row-42"]').text()).toContain('Does not match')
    expect(wrapper.get('[data-test="impact-count"]').text()).toBe('3 active accounts match')
  })

  it('keeps successful results but marks them out of date when the draft becomes invalid', async () => {
    post.mockResolvedValue(PAGE_ONE)
    const wrapper = mountPreview(PASSKEY_RULE)
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    await wrapper.setProps({ rule: INCOMPLETE_RULE })

    expect(wrapper.get('[data-test="impact-row-7"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="impact-status"]').text()).toBe('Impact preview is out of date.')
    expect(wrapper.get('[data-test="rule-impact-preview"]').attributes('data-state')).toBe('stale')
  })

  it('marks results stale after failure, offers Retry, and announces loading, current, and stale states', async () => {
    const first = deferred<RulePreviewPage>()
    post.mockReturnValueOnce(first.promise)
    const wrapper = mountPreview(PASSKEY_RULE)

    await vi.advanceTimersByTimeAsync(300)
    expect(wrapper.get('[data-test="impact-status"]').attributes('role')).toBe('status')
    expect(wrapper.get('[data-test="impact-status"]').text()).toBe('Updating impact preview…')

    first.resolve(PAGE_ONE)
    await flushPromises()
    expect(wrapper.get('[data-test="impact-status"]').text()).toBe('Impact preview is current.')

    post.mockRejectedValueOnce({ code: 'network_error' })
    await wrapper.setProps({ rule: FEDERATION_RULE })
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(wrapper.get('[data-test="impact-status"]').text()).toBe('Impact preview is out of date.')
    expect(wrapper.get('[data-test="impact-retry"]').text()).toBe('Retry preview')
    expect(wrapper.get('[data-test="impact-row-7"]').exists()).toBe(true)

    post.mockResolvedValueOnce(PAGE_TWO)
    await wrapper.get('[data-test="impact-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="impact-retry"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="impact-status"]').text()).toBe('Impact preview is current.')
  })
})
