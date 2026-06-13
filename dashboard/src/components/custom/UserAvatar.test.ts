import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import UserAvatar from './UserAvatar.vue'

describe('UserAvatar', () => {
  it('derives two initials from a multi-word display name', () => {
    const w = mount(UserAvatar, { props: { displayName: 'Alex Smith' } })
    expect(w.text()).toBe('AS')
  })

  it('uses the first two letters of a single-word display name', () => {
    const w = mount(UserAvatar, { props: { displayName: 'Alex' } })
    expect(w.text()).toBe('AL')
  })

  it('falls back to the username initials when displayName is blank', () => {
    const w = mount(UserAvatar, { props: { displayName: '   ', username: 'bob' } })
    expect(w.text()).toBe('BO')
  })

  it('renders a generic icon (no text) when both are empty', () => {
    const w = mount(UserAvatar, { props: { displayName: '', username: '' } })
    expect(w.text()).toBe('')
    expect(w.find('svg').exists()).toBe(true)
  })

  it('renders an <img> when src is provided', () => {
    const w = mount(UserAvatar, { props: { displayName: 'Alex Smith', src: '/avatar/x?v=ab' } })
    const img = w.find('img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toBe('/avatar/x?v=ab')
  })

  it('falls back to initials when the image errors', async () => {
    const w = mount(UserAvatar, { props: { displayName: 'Alex Smith', src: '/avatar/x?v=ab' } })
    await w.find('img').trigger('error')
    expect(w.find('img').exists()).toBe(false)
    expect(w.text()).toBe('AS')
  })
})
