import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { useBranding } from './useBranding'
import { testQueryClient } from '@/testSetup'
import { keys } from '@/queries/resources'
import { api } from '@/lib/api'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))
const Host = defineComponent({ setup: () => ({ branding: useBranding() }), template: '<div>{{ branding.instanceName }}|{{ branding.iconSrc }}|{{ branding.backgroundSrc }}</div>' })
describe('shared branding', () => {
  it('uses defaults while loading and after an initial error', async () => {
    vi.mocked(api.get).mockRejectedValue(new Error('offline'))
    const host = mount(Host)
    expect(host.text()).toBe('Prohibitorum|/branding/icon|/branding/background')
    await flushPromises()
    expect(host.text()).toBe('Prohibitorum|/branding/icon|/branding/background')
  })
  it('deduplicates consumers and updates names and asset versions together', async () => {
    vi.mocked(api.get).mockReset().mockResolvedValue({ instanceName: 'Acme', iconEtag: 'abcdef1234', backgroundEtag: 'bg123456xx' })
    const first = mount(Host); const second = mount(Host)
    await flushPromises()
    expect(api.get).toHaveBeenCalledTimes(1)
    expect(first.text()).toBe('Acme|/branding/icon?v=abcdef12|/branding/background?v=bg123456')
    testQueryClient.setQueryData(keys.config, { instanceName: 'Renamed', iconEtag: 'newimage', backgroundEtag: '' })
    await flushPromises()
    expect(first.text()).toBe('Renamed|/branding/icon?v=newimage|/branding/background')
    expect(second.text()).toBe(first.text())
    vi.mocked(api.get).mockRejectedValue(new Error('offline'))
    await testQueryClient.invalidateQueries({ queryKey: keys.config })
    expect(first.text()).toContain('Renamed')
  })
})
