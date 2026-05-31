<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
</script>

<template>
  <UCard>
    <h1 class="text-xl font-semibold">{{ t('consent.title') }}</h1>
  </UCard>
</template>
