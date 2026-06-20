<script setup lang="ts">
import { useTheme } from '@/composables/useTheme'
import { useLocale } from '@/composables/useLocale'
import SessionExpiredBanner from '@/components/custom/SessionExpiredBanner.vue'
useTheme()
useLocale()
</script>

<template>
  <SessionExpiredBanner />
  <RouterView />
</template>
