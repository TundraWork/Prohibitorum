import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import AppIcon from './AppIcon.vue'

describe('AppIcon', () => {
  it('renders the image when src is set', () => {
    const w = mount(AppIcon, { props: { src: '/icon/upstream_idp/google?v=abc', name: 'Google' } })
    expect(w.find('img').exists()).toBe(true)
    expect(w.find('img').attributes('src')).toContain('/icon/upstream_idp/google')
  })
  it('falls back to the initial when no src', () => {
    const w = mount(AppIcon, { props: { name: 'Google' } })
    expect(w.find('img').exists()).toBe(false)
    expect(w.text()).toBe('G')
  })
  it('falls back to the initial on image error', async () => {
    const w = mount(AppIcon, { props: { src: '/bad', name: 'Okta' } })
    await w.find('img').trigger('error')
    expect(w.find('img').exists()).toBe(false)
    expect(w.text()).toBe('O')
  })
})
