import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import PasswordCard from './PasswordCard.vue'

const post = vi.fn()
vi.mock('../../lib/api', () => ({ api: { post: (...a: unknown[]) => post(...a) } }))
vi.mock('../../lib/sudo', () => ({ withSudo: (fn: any) => fn() }))
beforeEach(() => post.mockReset())

describe('PasswordCard', () => {
  it('sets the password via withSudo', async () => {
    post.mockResolvedValueOnce(undefined)
    const w = mount(PasswordCard)
    await w.find('input[type="password"]').setValue('hunter2hunter2')
    await w.find('[data-test="save-password"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/password/set', { password: 'hunter2hunter2' })
    expect(w.text().toLowerCase()).toContain('updated')
  })
})
