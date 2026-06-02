import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import StatusBadge from './StatusBadge.vue'

describe('StatusBadge', () => {
  it('renders the kind label', () => {
    const w = mount(StatusBadge, { props: { kind: 'planned' } })
    expect(w.text().toLowerCase()).toContain('planned')
  })
})
