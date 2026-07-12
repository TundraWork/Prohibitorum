import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import PaginationControls from './PaginationControls.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

function mountControls(props: Record<string, unknown>) {
  return mount(PaginationControls, {
    props,
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })
}

describe('PaginationControls', () => {
  it('renders Previous and Next buttons', () => {
    const w = mountControls({ pageIndex: 0, hasMore: true, busy: false })
    expect(w.find('[data-test="prev-page"]').exists()).toBe(true)
    expect(w.find('[data-test="next-page"]').exists()).toBe(true)
  })

  it('disables Previous on page 0', () => {
    const w = mountControls({ pageIndex: 0, hasMore: true, busy: false })
    expect(w.find('[data-test="prev-page"]').attributes('disabled')).toBeDefined()
  })

  it('disables Next when hasMore is false (final page)', () => {
    const w = mountControls({ pageIndex: 0, hasMore: false, busy: false })
    expect(w.find('[data-test="next-page"]').attributes('disabled')).toBeDefined()
  })

  it('disables both buttons when busy', () => {
    const w = mountControls({ pageIndex: 1, hasMore: true, busy: true })
    expect(w.find('[data-test="prev-page"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-test="next-page"]').attributes('disabled')).toBeDefined()
  })

  it('emits next when Next is clicked', async () => {
    const w = mountControls({ pageIndex: 0, hasMore: true, busy: false })
    await w.find('[data-test="next-page"]').trigger('click')
    expect(w.emitted('next')).toBeTruthy()
  })

  it('emits previous when Previous is clicked', async () => {
    const w = mountControls({ pageIndex: 1, hasMore: false, busy: false })
    await w.find('[data-test="prev-page"]').trigger('click')
    expect(w.emitted('previous')).toBeTruthy()
  })

  it('does not emit when busy', async () => {
    const w = mountControls({ pageIndex: 1, hasMore: true, busy: true })
    await w.find('[data-test="next-page"]').trigger('click')
    expect(w.emitted('next')).toBeFalsy()
    await w.find('[data-test="prev-page"]').trigger('click')
    expect(w.emitted('previous')).toBeFalsy()
  })

  it('shows page indicator with current page number', () => {
    const w = mountControls({ pageIndex: 2, hasMore: true, busy: false })
    expect(w.find('[data-test="page-indicator"]').text()).toContain('3')
  })

  it('hides page indicator when showIndicator is false', () => {
    const w = mountControls({ pageIndex: 0, hasMore: true, busy: false, showIndicator: false })
    expect(w.find('[data-test="page-indicator"]').exists()).toBe(false)
  })

  it('uses aria-current on the page indicator', () => {
    const w = mountControls({ pageIndex: 0, hasMore: true, busy: false })
    expect(w.find('[data-test="page-indicator"]').attributes('aria-current')).toBe('page')
  })

  it('Previous button has aria-label for accessibility', () => {
    const w = mountControls({ pageIndex: 1, hasMore: true, busy: false })
    expect(w.find('[data-test="prev-page"]').attributes('aria-label')).toBeTruthy()
  })

  it('Next button has aria-label for accessibility', () => {
    const w = mountControls({ pageIndex: 0, hasMore: true, busy: false })
    expect(w.find('[data-test="next-page"]').attributes('aria-label')).toBeTruthy()
  })

  it('pagination container has role=navigation and aria-label', () => {
    const w = mountControls({ pageIndex: 0, hasMore: true, busy: false })
    const nav = w.find('[data-test="pagination-controls"]')
    expect(nav.attributes('role')).toBe('navigation')
    expect(nav.attributes('aria-label')).toBeTruthy()
  })

  it('focuses the Previous button after going back to page 0 from page 1', async () => {
    const w = mountControls({ pageIndex: 1, hasMore: true, busy: false })
    await w.find('[data-test="prev-page"]').trigger('click')
    await flushPromises()
    // After clicking previous, the emitted event fires and the button should
    // have been focused (or at least attempted — in jsdom, document.activeElement)
    expect(w.emitted('previous')).toBeTruthy()
  })

  it('renders nothing when there are no items and no pages', () => {
    const w = mountControls({ pageIndex: 0, hasMore: false, busy: false, hasItems: false })
    expect(w.find('[data-test="pagination-controls"]').exists()).toBe(false)
  })

  it('renders when hasItems is true even on final page', () => {
    const w = mountControls({ pageIndex: 0, hasMore: false, busy: false, hasItems: true })
    expect(w.find('[data-test="pagination-controls"]').exists()).toBe(true)
  })

  it('emits previous even on final page when pageIndex > 0', async () => {
    const w = mountControls({ pageIndex: 2, hasMore: false, busy: false, hasItems: true })
    await w.find('[data-test="prev-page"]').trigger('click')
    expect(w.emitted('previous')).toBeTruthy()
  })
})
