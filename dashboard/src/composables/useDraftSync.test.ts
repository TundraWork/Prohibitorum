import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { useDraftSync } from './useDraftSync'
describe('server-backed drafts', () => {
  it('refreshes pristine input, preserves edits, and accepts the latest server value on cancel/save', async () => {
    const host = mount(defineComponent({ setup() {
      const server = ref({ name: 'Initial' }), draft = ref('')
      const sync = useDraftSync(server, () => draft.value, value => { draft.value = value.name })
      return { server, draft, cancel: () => sync.accept(server.value) }
    }, template: '<input v-model="draft" />' }))
    expect(host.get('input').element.value).toBe('Initial')
    host.vm.server = { name: 'Background' }; await flushPromises()
    expect(host.get('input').element.value).toBe('Background')
    await host.get('input').setValue('My edits')
    host.vm.server = { name: 'Updated elsewhere' }; await flushPromises()
    expect(host.get('input').element.value).toBe('My edits')
    host.vm.cancel(); await flushPromises()
    expect(host.get('input').element.value).toBe('Updated elsewhere')
    host.vm.server = { name: 'Next update' }; await flushPromises()
    expect(host.get('input').element.value).toBe('Next update')
  })
})
