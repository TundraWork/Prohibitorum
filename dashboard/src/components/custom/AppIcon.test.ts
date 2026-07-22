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
    expect(w.attributes('style') ?? '').toContain('background-color')
  })
  it('rounds the image so it clips to the container (no corner bleed)', () => {
    const w = mount(AppIcon, { props: { src: '/icon/x', name: 'Y' } })
    expect(w.find('img').classes()).toContain('rounded-[inherit]')
  })
  it('bordered draws the inset ring on the icon element itself', () => {
    const w = mount(AppIcon, { props: { src: '/icon/x', name: 'Y', bordered: true } })
    expect(w.classes()).toEqual(expect.arrayContaining(['ring-1', 'ring-inset', 'ring-border']))
  })
})
