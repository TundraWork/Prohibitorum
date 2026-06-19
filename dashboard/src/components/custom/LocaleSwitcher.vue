<script setup lang="ts">
/**
 * LocaleSwitcher — switches the global app locale.
 *
 * Lists every locale registered on the i18n instance and binds the selection to
 * the global locale. Only `en` ships in Spec 1; the control is locale-count
 * agnostic, so `zh` (and any others) light up automatically once their strings
 * are authored — no change here.
 *
 * Implementation note: a token-styled native <select> is used rather than a
 * vendored shadcn Select — no Select primitive is vendored yet, and a native
 * control is fully accessible and keyboard-friendly out of the box. Swap for a
 * shadcn Select if/when the locale list grows enough to warrant a richer menu.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Languages } from 'lucide-vue-next'

const { t, locale, availableLocales } = useI18n({ useScope: 'global' })

/** Human-readable names for known locales; unknown codes fall back to the code. */
const LABELS: Record<string, string> = { en: 'English', zh: '中文' }
const options = computed(() =>
  availableLocales.map((code) => ({ value: code, label: LABELS[code] ?? code })),
)
</script>

<template>
  <label
    class="inline-flex items-center gap-1.5 rounded-md border border-border bg-surface px-2 py-1
           text-sm text-ink shadow-sm
           focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-0"
  >
    <Languages class="size-4 text-muted" aria-hidden="true" />
    <span class="sr-only">{{ t('common.language') }}</span>
    <select
      v-model="locale"
      :aria-label="t('common.language')"
      class="cursor-pointer appearance-none bg-transparent pr-1 outline-none"
    >
      <option v-for="opt in options" :key="opt.value" :value="opt.value">
        {{ opt.label }}
      </option>
    </select>
  </label>
</template>
