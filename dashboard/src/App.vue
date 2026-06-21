<script setup lang="ts">
import { watch } from 'vue'
import { useTheme } from '@/composables/useTheme'
import { useLocale } from '@/composables/useLocale'
import { useBrandingStore } from '@/stores/branding'
import { setFavicon } from '@/lib/favicon'
import SessionExpiredBanner from '@/components/custom/SessionExpiredBanner.vue'
useTheme()
useLocale()
const branding = useBrandingStore()
void branding.load()
// Keep the browser-tab favicon in sync with the instance icon. iconSrc carries
// a ?v=<etag> cache-buster, so an uploaded/removed icon changes the URL and the
// browser refetches instead of serving the stale (default) cached favicon.
watch(() => branding.iconSrc, setFavicon, { immediate: true })
</script>

<template>
  <SessionExpiredBanner />
  <RouterView />
</template>
