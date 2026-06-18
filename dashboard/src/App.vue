<script setup lang="ts">
import { useTheme } from '@/composables/useTheme'
import { useLocale } from '@/composables/useLocale'
useTheme()
useLocale()
</script>

<template>
  <RouterView />
</template>
