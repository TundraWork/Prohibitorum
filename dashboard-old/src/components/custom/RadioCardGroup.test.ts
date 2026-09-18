import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import RadioCardGroup from './RadioCardGroup.vue'

const OPTS = [
  { value: 'auto_provision', title: 'Auto-provision', description: 'Create accounts automatically.' },
  { value: 'invite_only', title: 'Invite only', description: 'Require an invitation.' },
  { value: 'link_only', title: 'Link only', description: 'Existing accounts only.' },
]

describe('RadioCardGroup', () => {
  it('renders a card per option with title and description', () => {
    const w = mount(RadioCardGroup, { props: { modelValue: 'auto_provision', options: OPTS }, attachTo: document.body })
    expect(w.find('[data-test="radio-card-auto_provision"]').exists()).toBe(true)
    expect(w.find('[data-test="radio-card-link_only"]').exists()).toBe(true)
    expect(w.text()).toContain('Invite only')
    expect(w.text()).toContain('Require an invitation.')
  })

  it('emits the selected value on card click', async () => {
    const w = mount(RadioCardGroup, { props: { modelValue: 'auto_provision', options: OPTS }, attachTo: document.body })
    await w.find('[data-test="radio-card-link_only"]').trigger('click')
    expect(w.emitted('update:modelValue')?.[0]).toEqual(['link_only'])
  })
})
