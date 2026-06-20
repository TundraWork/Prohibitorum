/**
 * Branding store — the instance name + icon, loaded once from the public
 * /config endpoint at boot. Drives the sidebar/login brand mark + page titles.
 * Defaults keep the UI sane before load() resolves (and if it fails).
 */
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '@/lib/api'

interface PublicConfig {
  instanceName: string
  hasCustomIcon: boolean
  iconUrl: string
  iconEtag: string
}

export const useBrandingStore = defineStore('branding', () => {
  const instanceName = ref('Prohibitorum')
  const hasCustomIcon = ref(false)
  const iconEtag = ref('')

  const iconSrc = computed(() => {
    const v = iconEtag.value ? iconEtag.value.slice(0, 8) : ''
    return v ? `/branding/icon?v=${v}` : '/branding/icon'
  })

  async function load(): Promise<void> {
    try {
      const cfg = await api.get<PublicConfig>('/api/prohibitorum/config')
      if (cfg.instanceName) instanceName.value = cfg.instanceName
      hasCustomIcon.value = !!cfg.hasCustomIcon
      iconEtag.value = cfg.iconEtag ?? ''
    } catch {
      // Keep defaults — branding is non-critical.
    }
  }

  return { instanceName, hasCustomIcon, iconEtag, iconSrc, load }
})
