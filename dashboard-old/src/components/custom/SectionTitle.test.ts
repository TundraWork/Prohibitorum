import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SectionTitle from './SectionTitle.vue'

describe('SectionTitle', () => {
  it('renders an h3 at the title tier by default', () => {
    const w = mount(SectionTitle, { slots: { default: 'Connection' } })
    const h3 = w.find('h3')
    expect(h3.exists()).toBe(true)
    expect(h3.text()).toBe('Connection')
    // Title tier: 16px / 600 / ink — distinct from the 14px/500 label tier.
    expect(h3.classes()).toEqual(
      expect.arrayContaining(['text-base', 'font-semibold', 'text-ink']),
    )
  })

  it('renders the requested heading level for correct nesting', () => {
    const w = mount(SectionTitle, { props: { as: 'h4' }, slots: { default: 'Delete' } })
    expect(w.find('h4').exists()).toBe(true)
    expect(w.find('h3').exists()).toBe(false)
  })

  it('merges caller classes alongside the tier classes', () => {
    const w = mount(SectionTitle, { props: { class: 'text-destructive' }, slots: { default: 'X' } })
    const el = w.find('h3')
    expect(el.classes()).toEqual(expect.arrayContaining(['text-base', 'font-semibold', 'text-destructive']))
  })
})
