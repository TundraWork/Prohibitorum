import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import BareCard from './BareCard.vue'

describe('BareCard', () => {
  it('renders a card narrowed to py-1 with the untitled marker', () => {
    const w = mount(BareCard, { slots: { default: 'Body' } })
    const card = w.find('[data-slot="card"]')
    expect(card.exists()).toBe(true)
    // twMerge does not preserve order — assert membership, never the full string.
    expect(card.classes()).toContain('py-1')
    expect(card.classes()).not.toContain('py-6')
    expect(card.attributes('data-untitled')).toBe('true')
  })

  it('lets the caller class win over the default padding', () => {
    const w = mount(BareCard, { props: { class: 'py-2' } })
    const card = w.find('[data-slot="card"]')
    expect(card.classes()).toContain('py-2')
    expect(card.classes()).not.toContain('py-1')
    expect(card.classes()).not.toContain('py-6')
  })

  it('passes default slot content through', () => {
    const w = mount(BareCard, { slots: { default: '<p data-test="body">Body</p>' } })
    expect(w.find('[data-test="body"]').text()).toBe('Body')
  })
})
