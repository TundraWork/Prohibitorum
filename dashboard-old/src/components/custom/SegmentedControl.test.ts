import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SegmentedControl from './SegmentedControl.vue'

const OPTS = [
  { value: 'user', label: 'User' },
  { value: 'admin', label: 'Admin' },
]

describe('SegmentedControl', () => {
  it('renders a segment per option', () => {
    const w = mount(SegmentedControl, { props: { modelValue: 'user', options: OPTS }, attachTo: document.body })
    expect(w.find('[data-test="segment-user"]').exists()).toBe(true)
    expect(w.find('[data-test="segment-admin"]').text()).toBe('Admin')
  })

  it('emits the selected value on segment click', async () => {
    const w = mount(SegmentedControl, { props: { modelValue: 'user', options: OPTS }, attachTo: document.body })
    await w.find('[data-test="segment-admin"]').trigger('click')
    expect(w.emitted('update:modelValue')?.[0]).toEqual(['admin'])
  })

  // Regression: the segments must signal clickability (cursor:hand), matching the
  // visually-identical TabsTrigger / role/binding toggles. Was missing — caught in review.
  it('each segment carries cursor-pointer', () => {
    const w = mount(SegmentedControl, { props: { modelValue: 'user', options: OPTS }, attachTo: document.body })
    for (const o of OPTS) {
      expect(w.find(`[data-test="segment-${o.value}"]`).classes()).toContain('cursor-pointer')
    }
  })
})
