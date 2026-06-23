<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{
  logoUri?: string
  displayName: string
  accountName: string
  policyUri?: string
  tosUri?: string
}>()
const { t } = useI18n()
</script>

<template>
  <div class="flex flex-col gap-6">
    <div class="flex flex-col items-center gap-2 text-center">
      <img v-if="logoUri" :src="logoUri" :alt="displayName" class="size-12 rounded-md object-contain" />
      <slot name="heading" />
      <p class="text-sm text-muted">{{ t('consent.yourAccount', { displayName: accountName }) }}</p>
    </div>

    <slot name="body" />
    <slot name="actions" />

    <p v-if="policyUri || tosUri" class="text-center text-xs text-muted">
      <a v-if="policyUri" :href="policyUri" target="_blank" rel="noopener noreferrer" class="underline-offset-4 hover:underline">{{ t('consent.privacyPolicy') }}</a>
      <span v-if="policyUri && tosUri"> &middot; </span>
      <a v-if="tosUri" :href="tosUri" target="_blank" rel="noopener noreferrer" class="underline-offset-4 hover:underline">{{ t('consent.termsOfService') }}</a>
    </p>
  </div>
</template>
