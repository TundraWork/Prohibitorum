<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import { useBrandingStore } from '@/stores/branding'

const { t } = useI18n()
const branding = useBrandingStore()
</script>

<template>
  <CenteredLayout large-interactive-targets>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">
        {{ t('vrchatProof.title') }}
      </h1>
    </template>

    <div class="flex flex-col gap-5">
      <p class="text-sm leading-5 text-ink">
        {{ t('vrchatProof.explanation', { instance: branding.instanceName }) }}
      </p>

      <section
        data-test="proof-guest-context"
        class="rounded-md border border-tide/20 bg-info/60 p-4"
        aria-labelledby="proof-guest-title"
      >
        <h2 id="proof-guest-title" class="text-sm font-semibold text-ink">
          {{ t('vrchatProof.guestTitle') }}
        </h2>
        <p class="mt-2 text-sm leading-5 text-muted">{{ t('vrchatProof.guestBody') }}</p>
      </section>

      <section
        data-test="proof-owner-instructions"
        class="flex flex-col gap-3 border-t border-border pt-5"
        aria-labelledby="proof-owner-title"
      >
        <h2 id="proof-owner-title" class="text-sm font-semibold text-ink">
          {{ t('vrchatProof.ownerTitle') }}
        </h2>
        <ol
          class="list-decimal space-y-2 ps-5 text-sm leading-5 text-ink"
          :aria-label="t('vrchatProof.instructionsLabel')"
        >
          <li>{{ t('vrchatProof.return', { instance: branding.instanceName }) }}</li>
          <li>{{ t('vrchatProof.remove', { instance: branding.instanceName }) }}</li>
          <li>{{ t('vrchatProof.close') }}</li>
        </ol>
      </section>
    </div>
  </CenteredLayout>
</template>
