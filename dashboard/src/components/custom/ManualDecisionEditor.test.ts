import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type { AccountSummary, ManualDecision } from '@/lib/appAccess'
import en from '@/locales/en'
import ManualDecisionEditor from './ManualDecisionEditor.vue'

const ALICE: AccountSummary = {
  id: 101,
  username: 'alice',
  displayName: 'Alice Avery',
}
const BOB: AccountSummary = {
  id: 102,
  username: 'bobby',
  displayName: 'Bob Baker',
}
const CAROL: AccountSummary = {
  id: 103,
  username: 'carol',
  displayName: 'Carol Chen',
}

const ALLOW_ALICE: ManualDecision = {
  account: ALICE,
  effect: 'allow',
  updatedAt: '2026-07-25T10:00:00Z',
}
const DENY_BOB: ManualDecision = {
  account: BOB,
  effect: 'deny',
  updatedAt: '2026-07-25T11:00:00Z',
}

const ACCOUNTS: AccountSummary[] = [ALICE, BOB, CAROL]
const DECISIONS: ManualDecision[] = [ALLOW_ALICE, DENY_BOB]

const makeI18n = () =>
  createI18n({
    legacy: false,
    locale: 'en',
    fallbackLocale: 'en',
    messages: { en },
  })

function mountEditor({
  decisions = DECISIONS,
  accounts = ACCOUNTS,
  busy = false,
}: {
  decisions?: ManualDecision[]
  accounts?: AccountSummary[]
  busy?: boolean
} = {}) {
  return mount(ManualDecisionEditor, {
    props: { decisions, accounts, busy },
    global: { plugins: [makeI18n()] },
    attachTo: document.body,
  })
}

describe('ManualDecisionEditor', () => {
  it('renders separate Allow and Deny views containing only their matching decisions', async () => {
    const w = mountEditor()
    const allowView = w.get('[data-test="manual-view-allow"]')
    const denyView = w.get('[data-test="manual-view-deny"]')

    expect(allowView.text()).toBe('Allow')
    expect(denyView.text()).toBe('Deny')
    expect(allowView.attributes('role')).toBe('tab')
    expect(allowView.attributes('aria-selected')).toBe('true')
    expect(w.get('[data-test="manual-list-allow"]').text()).toContain('Alice Avery')
    expect(w.get('[data-test="manual-list-allow"]').text()).not.toContain('Bob Baker')
    const denyListBefore = w.find('[data-test="manual-list-deny"]')
    expect(denyListBefore.exists() && denyListBefore.isVisible()).toBe(false)

    await denyView.trigger('mousedown')

    expect(denyView.attributes('aria-selected')).toBe('true')
    expect(allowView.attributes('aria-selected')).toBe('false')
    expect(w.get('[data-test="manual-list-deny"]').text()).toContain('Bob Baker')
    expect(w.get('[data-test="manual-list-deny"]').text()).not.toContain('Alice Avery')
  })

  it('filters account search case-insensitively by display name and username', async () => {
    const w = mountEditor({ decisions: [] })
    const search = w.get('[data-test="manual-account-search"]')

    await search.setValue('CAROL CH')
    expect(w.find('[data-test="manual-account-result-103"]').exists()).toBe(true)
    expect(w.find('[data-test="manual-account-result-101"]').exists()).toBe(false)
    expect(w.find('[data-test="manual-account-result-102"]').exists()).toBe(false)

    await search.setValue('bobby')
    expect(w.find('[data-test="manual-account-result-102"]').exists()).toBe(true)
    expect(w.find('[data-test="manual-account-result-103"]').exists()).toBe(false)
  })

  it('does not offer an account with either effect as a second decision', async () => {
    const w = mountEditor()
    const search = w.get('[data-test="manual-account-search"]')

    await search.setValue('alice')
    expect(w.find('[data-test="manual-account-result-101"]').exists()).toBe(false)

    await search.setValue('bobby')
    expect(w.find('[data-test="manual-account-result-102"]').exists()).toBe(false)

    await search.setValue('carol')
    expect(w.find('[data-test="manual-account-result-103"]').exists()).toBe(true)
  })

  it('sets one allow decision for a neutral account selected from search', async () => {
    const w = mountEditor({ decisions: [] })
    await w.get('[data-test="manual-account-search"]').setValue('carol')
    await w.get('[data-test="manual-set-allow-103"]').trigger('click')

    expect(w.emitted('set-decision')).toEqual([[{ accountId: 103, effect: 'allow' }]])
    expect(w.emitted('clear-decision')).toBeUndefined()
  })

  it('sets one deny decision for a neutral account selected from search', async () => {
    const w = mountEditor({ decisions: [] })
    await w.get('[data-test="manual-view-deny"]').trigger('mousedown')
    await w.get('[data-test="manual-account-search"]').setValue('carol')
    await w.get('[data-test="manual-set-deny-103"]').trigger('click')

    expect(w.emitted('set-decision')).toEqual([[{ accountId: 103, effect: 'deny' }]])
    expect(w.emitted('clear-decision')).toBeUndefined()
  })

  it('changes allow to deny with one set-decision emission and no clear', async () => {
    const w = mountEditor()
    await w.get('[data-test="manual-set-deny-101"]').trigger('click')

    expect(w.emitted('set-decision')).toEqual([[{ accountId: 101, effect: 'deny' }]])
    expect(w.emitted('clear-decision')).toBeUndefined()
  })

  it('changes deny to allow with one set-decision emission and no clear', async () => {
    const w = mountEditor()
    await w.get('[data-test="manual-view-deny"]').trigger('mousedown')
    await w.get('[data-test="manual-set-allow-102"]').trigger('click')

    expect(w.emitted('set-decision')).toEqual([[{ accountId: 102, effect: 'allow' }]])
    expect(w.emitted('clear-decision')).toBeUndefined()
  })

  it('clears a decision with one clear-decision emission and no set', async () => {
    const w = mountEditor()
    await w.get('[data-test="manual-clear-101"]').trigger('click')

    expect(w.emitted('clear-decision')).toEqual([[{ accountId: 101 }]])
    expect(w.emitted('set-decision')).toBeUndefined()
  })

  it('shows the exact neutral empty-state explanation', () => {
    const w = mountEditor({ decisions: [] })

    expect(w.get('[data-test="manual-neutral-copy"]').text()).toBe(
      'No manual decision; calculated groups decide.',
    )
  })

  it('explains allow claim precedence and deny issuance security', async () => {
    const w = mountEditor()

    expect(w.get('[data-test="manual-allow-explanation"]').text()).toBe(
      'Manual allow grants access. Matching rule groups still appear in claims.',
    )

    await w.get('[data-test="manual-view-deny"]').trigger('mousedown')
    expect(w.get('[data-test="manual-deny-explanation"]').text()).toBe(
      'Manual deny takes precedence. No token or assertion is issued.',
    )
  })

  it('marks itself busy and disables search and every visible mutation control', async () => {
    const w = mountEditor()
    await w.get('[data-test="manual-account-search"]').setValue('carol')
    await w.setProps({ busy: true })

    expect(w.get('[data-test="manual-decision-editor"]').attributes('aria-busy')).toBe('true')
    expect(w.get<HTMLInputElement>('[data-test="manual-account-search"]').element.disabled).toBe(true)
    expect(w.get<HTMLButtonElement>('[data-test="manual-set-allow-103"]').element.disabled).toBe(true)
    expect(w.get<HTMLButtonElement>('[data-test="manual-set-deny-101"]').element.disabled).toBe(true)
    expect(w.get<HTMLButtonElement>('[data-test="manual-clear-101"]').element.disabled).toBe(true)
    expect(w.emitted('set-decision')).toBeUndefined()
    expect(w.emitted('clear-decision')).toBeUndefined()
  })

  it('gives search and account mutation controls specific accessible labels', async () => {
    const w = mountEditor()
    const search = w.get('[data-test="manual-account-search"]')

    expect(search.attributes('aria-label')).toBe('Search accounts')
    expect(w.get('[data-test="manual-set-deny-101"]').attributes('aria-label')).toBe(
      'Change Alice Avery from allow to deny',
    )
    expect(w.get('[data-test="manual-clear-101"]').attributes('aria-label')).toBe(
      'Clear manual decision for Alice Avery',
    )

    await search.setValue('carol')
    expect(w.get('[data-test="manual-set-allow-103"]').attributes('aria-label')).toBe(
      'Allow Carol Chen',
    )

    await w.get('[data-test="manual-view-deny"]').trigger('mousedown')
    expect(w.get('[data-test="manual-set-allow-102"]').attributes('aria-label')).toBe(
      'Change Bob Baker from deny to allow',
    )
  })

  it('uses focusable native buttons for keyboard-triggerable view and mutation controls', async () => {
    const w = mountEditor()
    await w.get('[data-test="manual-account-search"]').setValue('carol')

    const selectors = [
      '[data-test="manual-view-allow"]',
      '[data-test="manual-view-deny"]',
      '[data-test="manual-set-allow-103"]',
      '[data-test="manual-set-deny-101"]',
      '[data-test="manual-clear-101"]',
    ]

    for (const selector of selectors) {
      const control = w.get<HTMLButtonElement>(selector)
      expect(control.element.tagName).toBe('BUTTON')
      expect(control.attributes('type')).toBe('button')
      control.element.focus()
      expect(document.activeElement).toBe(control.element)
    }
  })
})
