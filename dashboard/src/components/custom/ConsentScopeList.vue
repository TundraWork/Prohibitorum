<script setup lang="ts">
/**
 * ConsentScopeList — renders the scopes an OIDC client is requesting.
 *
 * Each scope is described via consent.scopes.<scope> i18n copy; an unknown /
 * technical scope falls back to its raw value in mono (Code-Gets-Mono rule),
 * so a relying party requesting a custom scope still shows something honest
 * rather than a blank or a misleading guess.
 */
import { useI18n } from 'vue-i18n'
import { Check } from 'lucide-vue-next'

defineProps<{ scopes: string[] }>()

const { t, te } = useI18n()
const isKnown = (scope: string) => te(`consent.scopes.${scope}`)
</script>

<template>
  <ul class="flex flex-col gap-2">
    <li v-for="scope in scopes" :key="scope" class="flex items-start gap-2 text-sm text-ink">
      <Check class="mt-0.5 size-4 shrink-0 text-tide" aria-hidden="true" />
      <span v-if="isKnown(scope)">{{ t(`consent.scopes.${scope}`) }}</span>
      <span v-else class="flex flex-col gap-0.5">
        <code class="font-mono text-muted">{{ scope }}</code>
        <span class="text-xs text-muted">{{ t('consent.customScope') }}</span>
      </span>
    </li>
  </ul>
</template>
