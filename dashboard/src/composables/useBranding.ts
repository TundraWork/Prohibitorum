import { computed, reactive } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { configQuery } from '@/queries/resources'

export function useBranding() {
  const config = useQuery(configQuery())
  const iconEtag = computed(() => config.data.value?.iconEtag ?? '')
  const backgroundEtag = computed(() => config.data.value?.backgroundEtag ?? '')
  return reactive({
    instanceName: computed(() => config.data.value?.instanceName || 'Prohibitorum'),
    hasCustomIcon: computed(() => config.data.value?.hasCustomIcon ?? false), iconEtag,
    iconSrc: computed(() => '/branding/icon' + (iconEtag.value ? '?v=' + iconEtag.value.slice(0, 8) : '')),
    maintenanceMode: computed(() => config.data.value?.maintenanceMode ?? false),
    maintenanceMessage: computed(() => config.data.value?.maintenanceMessage ?? ''),
    hasCustomBackground: computed(() => config.data.value?.hasCustomBackground ?? false), backgroundEtag,
    backgroundSrc: computed(() => '/branding/background' + (backgroundEtag.value ? '?v=' + backgroundEtag.value.slice(0, 8) : '')),
    error: config.error,
  })
}
