<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import CenteredLayout from '@/pages/CenteredLayout.vue'

const { t } = useI18n()
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">
        {{ t('vrchatProof.title') }}
      </h1>
    </template>

    <div class="flex flex-col gap-5">
      <p class="text-sm text-ink">{{ t('vrchatProof.explanation') }}</p>
      <p class="text-sm text-muted">{{ t('vrchatProof.invariant') }}</p>

      <ol
        class="list-decimal space-y-3 ps-5 text-sm text-ink"
        :aria-label="t('vrchatProof.instructionsLabel')"
      >
        <li>{{ t('vrchatProof.return') }}</li>
        <li>{{ t('vrchatProof.remove') }}</li>
        <li>{{ t('vrchatProof.close') }}</li>
      </ol>
    </div>
  </CenteredLayout>
</template>
