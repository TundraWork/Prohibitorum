import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { Inbox } from 'lucide-vue-next'
import EmptyState from './EmptyState.vue'

describe('EmptyState', () => {
  it('renders title and description with role=status', () => {
    const w = mount(EmptyState, {
      props: { title: 'No items yet.', description: 'Create one to get started.' },
    })
    expect(w.attributes('role')).toBe('status')
    expect(w.text()).toContain('No items yet.')
    expect(w.text()).toContain('Create one to get started.')
  })

  it('omits description when not provided', () => {
    const w = mount(EmptyState, { props: { title: 'Nothing here.' } })
    const paras = w.findAll('p')
    expect(paras).toHaveLength(1)
    expect(paras[0].text()).toBe('Nothing here.')
  })

  it('renders slotted CTA', () => {
    const w = mount(EmptyState, {
      props: { title: 'Empty' },
      slots: { default: '<button data-test="cta">Add item</button>' },
    })
    expect(w.find('button[data-test="cta"]').exists()).toBe(true)
    expect(w.find('button[data-test="cta"]').text()).toBe('Add item')
  })

  it('renders the icon when provided', () => {
    const w = mount(EmptyState, {
      props: { title: 'Empty', icon: Inbox },
    })
    // lucide renders an svg
    expect(w.find('svg').exists()).toBe(true)
    expect(w.find('svg').attributes('aria-hidden')).toBe('true')
  })
})
