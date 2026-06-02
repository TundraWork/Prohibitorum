import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import RecoveryCodesCard from './RecoveryCodesCard.vue'

const post = vi.fn()
vi.mock('../../lib/api', () => ({ api: { post: (...a: unknown[]) => post(...a) } }))
vi.mock('../../lib/sudo', () => ({ withSudo: (fn: any) => fn() }))
beforeEach(() => post.mockReset())

describe('RecoveryCodesCard', () => {
  it('regenerates after confirm and shows codes', async () => {
    post.mockResolvedValueOnce({ recovery_codes: ['aaaa-bbbb', 'cccc-dddd'] })
    const w = mount(RecoveryCodesCard)
    await w.find('[data-test="regen"]').trigger('click') // arm
    await w.find('[data-test="regen"]').trigger('click') // confirm
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/recovery-codes/regenerate')
    expect(w.text()).toContain('aaaa-bbbb')
  })
})
