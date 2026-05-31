<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
</script>

<template>
  <UCard>
    <h1 class="text-xl font-semibold">{{ t('error.title') }}</h1>
    <p class="mt-2 text-muted">{{ t('error.generic') }}</p>
  </UCard>
</template>
