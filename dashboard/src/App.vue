<script setup lang="ts">
import { watch } from 'vue'
import { useTheme } from '@/composables/useTheme'
import { useLocale } from '@/composables/useLocale'
import { useBrandingStore } from '@/stores/branding'
import { setFavicon } from '@/lib/favicon'
import SessionExpiredBanner from '@/components/custom/SessionExpiredBanner.vue'
import OfflineBanner from '@/components/custom/OfflineBanner.vue'
import Toaster from '@/components/custom/Toaster.vue'
useTheme()
useLocale()
const branding = useBrandingStore()
// ensureLoaded() memoises the load — calling it here sets the _loadedFlag so
// the router guard's await resolves immediately after App.vue boots.
void branding.ensureLoaded()
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
