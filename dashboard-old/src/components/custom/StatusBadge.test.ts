import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import StatusBadge from './StatusBadge.vue'

const cls = (variant?: string) =>
  mount(StatusBadge, { props: variant ? { variant } : {}, slots: { default: 'X' } }).classes().join(' ')

describe('StatusBadge', () => {
  it('defaults to neutral', () => {
    expect(cls()).toContain('bg-sunken')
    expect(cls()).toContain('text-muted')
  })
  it('success uses the -50/-700 pair', () => {
    expect(cls('success')).toContain('bg-sage-50')
    expect(cls('success')).toContain('text-sage-700')
  })
  it('caution uses amber-700 text (NOT the L0.76 amber-500)', () => {
    const c = cls('caution')
    expect(c).toContain('bg-amber-50')
    expect(c).toContain('text-amber-700')
    // Negative lookahead rejects the bare text-amber (L0.76) alias but allows text-amber-700.
    expect(c).not.toMatch(/\btext-amber(?!-)\b/)
  })
  it('danger uses the -50/-700 pair', () => {
    expect(cls('danger')).toContain('bg-rose-50')
    expect(cls('danger')).toContain('text-rose-700')
  })
  it('info uses the dark-aware info tokens', () => {
    expect(cls('info')).toContain('bg-info')
    expect(cls('info')).toContain('text-info-foreground')
  })
})
