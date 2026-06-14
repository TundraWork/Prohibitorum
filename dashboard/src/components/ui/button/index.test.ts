import { describe, it, expect } from 'vitest'
import { buttonVariants } from './index'

describe('buttonVariants', () => {
  it('always includes cursor-pointer (interactive affordance)', () => {
    expect(buttonVariants()).toContain('cursor-pointer')
    expect(buttonVariants({ variant: 'ghost', size: 'icon' })).toContain('cursor-pointer')
    expect(buttonVariants({ variant: 'destructive', size: 'lg' })).toContain('cursor-pointer')
  })
})
