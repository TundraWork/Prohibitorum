/**
 * Branding store — the instance name + icon + maintenance mode, loaded once
 * from the public /config endpoint at boot. Drives the sidebar/login brand mark
 * + page titles + the maintenance gate. Defaults keep the UI sane before
 * load() resolves (and if it fails).
 */
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '@/lib/api'

interface PublicConfig {
  instanceName: string
  hasCustomIcon: boolean
  iconUrl: string
  iconEtag: string
  maintenanceMode: boolean
  maintenanceMessage: string
  hasCustomBackground: boolean
  backgroundUrl: string
  backgroundEtag: string
}

export const useBrandingStore = defineStore('branding', () => {
  const instanceName = ref('Prohibitorum')
  const hasCustomIcon = ref(false)
  const iconEtag = ref('')
  const maintenanceMode = ref(false)
  const maintenanceMessage = ref('')
  const hasCustomBackground = ref(false)
  const backgroundEtag = ref('')

  const iconSrc = computed(() => {
    const v = iconEtag.value ? iconEtag.value.slice(0, 8) : ''
    return v ? `/branding/icon?v=${v}` : '/branding/icon'
  })

  const backgroundSrc = computed(() => {
    const v = backgroundEtag.value ? backgroundEtag.value.slice(0, 8) : ''
    return v ? `/branding/background?v=${v}` : '/branding/background'
  })

  async function load(): Promise<void> {
    try {
      const cfg = await api.get<PublicConfig>('/api/prohibitorum/config')
      if (cfg.instanceName) instanceName.value = cfg.instanceName
      hasCustomIcon.value = !!cfg.hasCustomIcon
      iconEtag.value = cfg.iconEtag ?? ''
      maintenanceMode.value = !!cfg.maintenanceMode
      maintenanceMessage.value = cfg.maintenanceMessage ?? ''
      hasCustomBackground.value = !!cfg.hasCustomBackground
      backgroundEtag.value = cfg.backgroundEtag ?? ''
    } catch {
      // Keep defaults — branding is non-critical.
    } finally {
      _loadedFlag.value = true
    }
  }

  // Memoized load — callers (router guard) can await this and it resolves
  // immediately if load() has already run. App.vue calls ensureLoaded() at boot.
  const _loadedFlag = ref(false)
  let _loadPromise: Promise<void> | null = null

  async function ensureLoaded(): Promise<void> {
    if (_loadedFlag.value) return
    if (!_loadPromise) _loadPromise = load()
    await _loadPromise
  }

  return {
    instanceName, hasCustomIcon, iconEtag, iconSrc,
    maintenanceMode, maintenanceMessage,
    hasCustomBackground, backgroundEtag, backgroundSrc,
    load, ensureLoaded,
  }
})
