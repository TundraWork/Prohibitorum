import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import BackLink from './BackLink.vue'

describe('BackLink', () => {
  it('renders label text and to prop', () => {
    const w = mount(BackLink, {
      props: { to: '/admin/accounts', label: 'Back to accounts' },
      global: {
        stubs: { RouterLink: { template: '<a :href="to"><slot /></a>', props: ['to'] } },
      },
    })
    expect(w.text()).toContain('Back to accounts')
    expect(w.find('a').attributes('href')).toBe('/admin/accounts')
  })

  it('includes a ChevronLeft icon', () => {
    const w = mount(BackLink, {
      props: { to: '/admin/accounts', label: 'Back' },
      global: {
        stubs: { RouterLink: { template: '<a :href="to"><slot /></a>', props: ['to'] } },
      },
    })
    expect(w.find('svg').exists()).toBe(true)
    expect(w.find('svg').attributes('aria-hidden')).toBe('true')
  })
})
