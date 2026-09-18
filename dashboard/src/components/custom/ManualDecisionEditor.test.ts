import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import type { ManualDecision } from '@/lib/appAccess'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))

import { api } from '@/lib/api'
import ManualDecisionEditor from './ManualDecisionEditor.vue'

const get = vi.mocked(api.get)

const ALLOW_ALICE: ManualDecision = {
  account: { id: 101, username: 'alice', displayName: 'Alice Avery' },
  effect: 'allow',
  updatedAt: '2026-07-25T10:00:00Z',
}
const DENY_BOB: ManualDecision = {
  account: { id: 102, username: 'bobby', displayName: 'Bob Baker' },
  effect: 'deny',
  updatedAt: '2026-07-25T11:00:00Z',
}
const DECISIONS = [ALLOW_ALICE, DENY_BOB]

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

function mountEditor(decisions: ManualDecision[] = DECISIONS, busy = false) {
  return mount(ManualDecisionEditor, {
    props: { decisions, busy },
    global: { plugins: [makeI18n()] },
    attachTo: document.body,
  })
}

async function search(wrapper: VueWrapper, query: string): Promise<void> {
  const input = wrapper.get<HTMLInputElement>('[data-test="manual-account-search"]')
  await input.setValue(query)
  await input.trigger('keydown', { key: 'Enter' })
  await flushPromises()
}

beforeEach(() => {
  get.mockReset()
  document.body.innerHTML = ''
})

afterEach(() => {
  document.body.innerHTML = ''
})

describe('ManualDecisionEditor', () => {
  it('renders allow and deny decisions as semantic tables with timestamps', async () => {
    const wrapper = mountEditor()
    const allow = wrapper.get('[data-test="manual-list-allow"]')
    expect(allow.find('table').exists()).toBe(true)
    expect(allow.text()).toContain('Alice Avery')
    expect(allow.text()).toContain('alice')
    expect(allow.get('time').attributes('datetime')).toBe(ALLOW_ALICE.updatedAt)
    expect(allow.find('[data-test="manual-set-deny-101"]').exists()).toBe(false)

    await wrapper.get('[data-test="manual-view-deny"]').trigger('mousedown')
    const deny = wrapper.get('[data-test="manual-list-deny"]')
    expect(deny.find('table').exists()).toBe(true)
    expect(deny.text()).toContain('Bob Baker')
    expect(deny.find('[data-test="manual-set-allow-102"]').exists()).toBe(false)
  })

  it('uses the approved explanations and empty state copy', async () => {
    const wrapper = mountEditor([])
    expect(wrapper.get('[data-test="manual-allow-explanation"]').text()).toBe(
      'Manual allow grants access. Matching calculated groups still appear in downstream claims.',
    )
    expect(wrapper.get('[data-test="manual-neutral-copy"]').text()).toBe('No related users')

    await wrapper.get('[data-test="manual-view-deny"]').trigger('mousedown')
    expect(wrapper.get('[data-test="manual-deny-explanation"]').text()).toBe(
      'Manual deny takes precedence. No token or assertion is issued.',
    )
  })

  it('clears a decision through its only row action', async () => {
    const wrapper = mountEditor()
    const clear = wrapper.get<HTMLButtonElement>('[data-test="manual-clear-101"]')
    expect(clear.attributes('aria-label')).toBe('Clear manual decision for Alice Avery')
    expect(clear.attributes('title')).toBe('Clear manual decision for Alice Avery')
    expect(clear.text()).toBe('')

    await clear.trigger('click')
    expect(wrapper.emitted('clear-decision')).toEqual([[{ accountId: 101 }]])
    expect(wrapper.emitted('set-decision')).toBeUndefined()
  })

  it('searches the shared directory only after submit and excludes disabled or decided accounts', async () => {
    get.mockResolvedValue({
      items: [
        { id: 101, username: 'alice', displayName: 'Alice Avery', disabled: false },
        { id: 103, username: 'carol', displayName: 'Carol Chen', disabled: false },
        { id: 104, username: 'disabled', displayName: 'Disabled', disabled: true },
      ],
      nextCursor: '',
    })
    const wrapper = mountEditor()
    await wrapper.get('[data-test="manual-account-search"]').setValue(' engineer ')
    expect(get).not.toHaveBeenCalled()

    await wrapper.get('form[role="search"]').trigger('submit')
    await flushPromises()

    expect(get).toHaveBeenCalledWith(
      '/api/prohibitorum/accounts?q=engineer&limit=100',
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
    expect(wrapper.find('[data-test="manual-account-result-101"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="manual-account-result-104"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="manual-account-result-103"]').text()).toContain('Carol Chen')
  })

  it('adds a searched account to the active effect with a primary explicit action', async () => {
    get.mockResolvedValue({
      items: [{ id: 103, username: 'carol', displayName: 'Carol Chen', disabled: false }],
      nextCursor: '',
    })
    const wrapper = mountEditor([])
    await search(wrapper, 'carol')

    const allow = wrapper.get<HTMLButtonElement>('[data-test="manual-account-select-103"]')
    expect(allow.text()).toBe('Add to allow')
    expect(allow.attributes('aria-label')).toBe('Allow Carol Chen')
    expect(allow.attributes('data-variant')).toBe('default')
    await allow.trigger('click')
    expect(wrapper.emitted('set-decision')).toEqual([[{ accountId: 103, effect: 'allow' }]])

    await wrapper.get('[data-test="manual-view-deny"]').trigger('mousedown')
    await search(wrapper, 'carol')
    const deny = wrapper.get<HTMLButtonElement>('[data-test="manual-account-select-103"]')
    expect(deny.text()).toBe('Add to deny')
    await deny.trigger('click')
    expect(wrapper.emitted('set-decision')?.[1]).toEqual([{ accountId: 103, effect: 'deny' }])
  })

  it('pages through account search results', async () => {
    get.mockImplementation(async (path: string) => {
      if (path === '/api/prohibitorum/accounts?q=engineer&limit=100') {
        return { items: [{ id: 103, username: 'carol', displayName: 'Carol Chen', disabled: false }], nextCursor: 'next/page' }
      }
      if (path === '/api/prohibitorum/accounts?q=engineer&limit=100&cursor=next%2Fpage') {
        return { items: [{ id: 105, username: 'grace', displayName: 'Grace Hopper', disabled: false }], nextCursor: '' }
      }
      throw new Error('Unexpected GET ' + path)
    })
    const wrapper = mountEditor([])
    await search(wrapper, 'engineer')
    await wrapper.get('[data-test="next-page"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="manual-account-result-103"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="manual-account-result-105"]').text()).toContain('Grace Hopper')
    expect(wrapper.get('[data-test="page-indicator"]').text()).toContain('2')
  })

  it('disables visible mutations while busy', async () => {
    get.mockResolvedValue({
      items: [{ id: 103, username: 'carol', displayName: 'Carol Chen', disabled: false }],
      nextCursor: '',
    })
    const wrapper = mountEditor()
    await search(wrapper, 'carol')
    await wrapper.setProps({ busy: true })

    expect(wrapper.get('[data-test="manual-decision-editor"]').attributes('aria-busy')).toBe('true')
    expect(wrapper.get<HTMLButtonElement>('[data-test="manual-clear-101"]').element.disabled).toBe(true)
    expect(wrapper.get<HTMLInputElement>('[data-test="manual-account-search"]').element.disabled).toBe(true)
    expect(wrapper.get<HTMLButtonElement>('form[role="search"] button[type="submit"]').element.disabled).toBe(true)
    expect(wrapper.get<HTMLButtonElement>('[data-test="manual-account-select-103"]').element.disabled).toBe(true)
  })
})
