import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import StatusMessage from './StatusMessage.vue'

describe('StatusMessage', () => {
  it('is a persistent role=status region, sr-only and empty when idle', () => {
    const w = mount(StatusMessage, { props: { show: false }, slots: { default: 'Saved' } })
    const region = w.find('[role="status"]')
    expect(region.exists()).toBe(true) // persistent — stays in the a11y tree
    expect(region.classes()).toContain('sr-only')
    expect(region.text()).toBe('') // no content when idle → the show=true change announces
  })

  it('shows the message in the success tone when shown', () => {
    const w = mount(StatusMessage, { props: { show: true }, slots: { default: 'Saved' } })
    const region = w.find('[role="status"]')
    expect(region.text()).toBe('Saved')
    expect(region.classes()).toEqual(expect.arrayContaining(['text-sm', 'text-sage-700']))
    expect(region.classes()).not.toContain('sr-only')
  })

  it('merges caller classes when shown', () => {
    const w = mount(StatusMessage, { props: { show: true, class: 'text-center' }, slots: { default: 'Done' } })
    expect(w.find('[role="status"]').classes()).toEqual(
      expect.arrayContaining(['text-sm', 'text-sage-700', 'text-center']),
    )
  })
})
