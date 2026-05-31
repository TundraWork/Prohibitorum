<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
</script>

<template>
  <UCard>
    <h1 class="text-xl font-semibold">{{ t('login.title') }}</h1>
    <UButton class="mt-4" block>{{ t('login.passkey') }}</UButton>
  </UCard>
</template>
