<script setup lang="ts">
import { watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { buildTitle } from '@/lib/pageTitle'
import { useTheme } from '@/composables/useTheme'
import { useLocale } from '@/composables/useLocale'
import { useBranding } from '@/composables/useBranding'
import { setFavicon } from '@/lib/favicon'
import SessionExpiredBanner from '@/components/custom/SessionExpiredBanner.vue'
import OfflineBanner from '@/components/custom/OfflineBanner.vue'
import Toaster from '@/components/custom/Toaster.vue'
useTheme()
useLocale()
const branding = useBranding()
const route = useRoute()
const { t, locale } = useI18n()
watch(() => [branding.instanceName, route.meta.titleKey, locale.value], () => {
  document.title = buildTitle(route.meta.titleKey ? t(route.meta.titleKey) : '', branding.instanceName)
}, { immediate: true })
// Keep the browser-tab favicon in sync with the instance icon. iconSrc carries
// a ?v=<etag> cache-buster, so an uploaded/removed icon changes the URL and the
// browser refetches instead of serving the stale (default) cached favicon.
watch(() => branding.iconSrc, setFavicon, { immediate: true })
</script>

<template>
  <OfflineBanner />
  <SessionExpiredBanner />
  <RouterView />
  <Toaster />
</template>
