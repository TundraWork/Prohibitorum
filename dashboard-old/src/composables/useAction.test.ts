import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { useAction } from './useAction'
import { clearSessionQueries } from '@/queries/client'
import { testQueryClient } from '@/testSetup'
const Host = defineComponent({ setup: () => useAction('accounts'), template: '<div />' })
describe('explicit actions', () => {
  it('cancels result delivery on session cleanup, even if a transport ignores abort', async () => {
    const host = mount(Host)
    let finish!: (value: string) => void
    let signal!: AbortSignal
    const delivered = vi.fn()
    const result = host.vm.execute(s => { signal = s; return new Promise<string>(resolve => { finish = resolve }) }).then(delivered).catch(error => error)
    await flushPromises()
    await clearSessionQueries(testQueryClient)
    expect(signal.aborted).toBe(true)
    finish('one-time secret')
    expect((await result).name).toBe('AbortError')
    expect(delivered).not.toHaveBeenCalled()
    expect(testQueryClient.getMutationCache().getAll()).toHaveLength(0)
  })
  it('resets secret results after consumption and aborts when the view leaves', async () => {
    const host = mount(Host)
    expect(await host.vm.execute(async () => 'secret')).toBe('secret')
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(testQueryClient.getMutationCache().getAll()).toHaveLength(0)
    let finish!: () => void
    const pending = host.vm.execute(() => new Promise<void>(resolve => { finish = resolve })).catch(error => error)
    await flushPromises(); host.unmount(); finish()
    expect((await pending).name).toBe('AbortError')
  })
})
