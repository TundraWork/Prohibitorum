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
  it('renders the brand mark for a known protocol, ignoring src + initial', () => {
    const w = mount(AppIcon, { props: { protocol: 'vrchat', src: '/icon/upstream_idp/x', name: 'VRChat' } })
    expect(w.find('img').attributes('src')).toContain('vrchat-logo')
    expect(w.text()).toBe('') // no initial letter
    expect(w.classes()).not.toContain('bg-accent') // brand colour, not the grey chip
    expect(w.attributes('style') ?? '').toContain('background-color')
  })
  it('does NOT put a grey background behind an uploaded icon', () => {
    const w = mount(AppIcon, { props: { src: '/icon/x', name: 'Y' } })
    expect(w.find('img').exists()).toBe(true)
    expect(w.classes()).not.toContain('bg-accent')
  })
  it('uses the grey chip ONLY for the no-icon placeholder', () => {
    const w = mount(AppIcon, { props: { name: 'Y' } })
    expect(w.find('img').exists()).toBe(false)
    expect(w.classes()).toContain('bg-accent')
  })
  it('renders an uploaded icon object-contain (margin, not cropped)', () => {
    const w = mount(AppIcon, { props: { src: '/icon/x', name: 'Y' } })
    expect(w.find('img').classes()).toContain('object-contain')
  })
})
