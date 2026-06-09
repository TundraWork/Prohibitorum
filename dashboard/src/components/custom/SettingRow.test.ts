import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SettingRow from './SettingRow.vue'

describe('SettingRow', () => {
  it('renders the label, description, and slotted control with a matching for/id', () => {
    const w = mount(SettingRow, {
      props: { label: 'Require verified email', description: 'Only verified addresses.', for: 'rve' },
      slots: { default: '<button id="rve">toggle</button>' },
    })
    const label = w.find('label')
    expect(label.text()).toBe('Require verified email')
    expect(label.attributes('for')).toBe('rve')
    expect(w.text()).toContain('Only verified addresses.')
    expect(w.find('button#rve').exists()).toBe(true)
  })

  it('omits the description when not provided', () => {
    const w = mount(SettingRow, { props: { label: 'Disabled', for: 'd' }, slots: { default: '<i id="d" />' } })
    expect(w.find('p').exists()).toBe(false)
  })
})
