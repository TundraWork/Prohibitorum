<script setup lang="ts">
import { useTheme } from '@/composables/useTheme'
import { useLocale } from '@/composables/useLocale'
import { useBrandingStore } from '@/stores/branding'
import SessionExpiredBanner from '@/components/custom/SessionExpiredBanner.vue'
useTheme()
useLocale()
void useBrandingStore().load()
</script>

<template>
  <SessionExpiredBanner />
  <RouterView />
</template>
