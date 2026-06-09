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
})
