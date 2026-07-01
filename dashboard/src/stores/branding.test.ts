import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useBrandingStore } from './branding'
import { api } from '@/lib/api'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))

describe('branding store', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('defaults to Prohibitorum, no custom icon', () => {
    const b = useBrandingStore()
    expect(b.instanceName).toBe('Prohibitorum')
    expect(b.hasCustomIcon).toBe(false)
    expect(b.iconSrc).toBe('/branding/icon')
  })

  it('load() populates from /config and builds a cache-busted iconSrc', async () => {
    vi.mocked(api.get).mockResolvedValue({ instanceName: 'Acme SSO', hasCustomIcon: true, iconUrl: '/branding/icon', iconEtag: 'abcdef1234' })
    const b = useBrandingStore()
    await b.load()
    expect(b.instanceName).toBe('Acme SSO')
    expect(b.hasCustomIcon).toBe(true)
    expect(b.iconSrc).toBe('/branding/icon?v=abcdef12')
  })

  it('keeps defaults if /config fails', async () => {
    vi.mocked(api.get).mockRejectedValue(new Error('net'))
    const b = useBrandingStore()
    await b.load()
    expect(b.instanceName).toBe('Prohibitorum')
  })

  it('defaults to no custom background', () => {
    const b = useBrandingStore()
    expect(b.hasCustomBackground).toBe(false)
    expect(b.backgroundSrc).toBe('/branding/background')
  })

  it('load() builds a cache-busted backgroundSrc', async () => {
    vi.mocked(api.get).mockResolvedValue({
      instanceName: 'Acme SSO', hasCustomIcon: false, iconUrl: '/branding/icon', iconEtag: '',
      hasCustomBackground: true, backgroundUrl: '/branding/background', backgroundEtag: 'bg123456xx',
    })
    const b = useBrandingStore()
    await b.load()
    expect(b.hasCustomBackground).toBe(true)
    expect(b.backgroundSrc).toBe('/branding/background?v=bg123456')
  })
})
