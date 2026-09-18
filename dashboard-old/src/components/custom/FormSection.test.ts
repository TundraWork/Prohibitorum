import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import FormSection from './FormSection.vue'

describe('FormSection', () => {
  it('renders the title and slotted content', () => {
    const w = mount(FormSection, {
      props: { title: 'Connection', description: 'How we reach the provider.' },
      slots: { default: '<input name="issuer" />' },
    })
    expect(w.find('h3').text()).toBe('Connection')
    expect(w.text()).toContain('How we reach the provider.')
    expect(w.find('input[name="issuer"]').exists()).toBe(true)
  })
})
