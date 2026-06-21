<script setup lang="ts">
/**
 * LocaleSwitcher — switches the global app locale.
 *
 * Built on the vendored shadcn-vue `Select` (the project's standard value-picker,
 * as on AppAccessCard): a real button trigger with a full clickable region and a
 * chevron affordance, and a PORTALED popup that escapes the sidebar's overflow
 * and matches the app's other dropdowns — instead of an `appearance-none` native
 * <select> (no chevron, OS-native popup). Locale-count agnostic: every locale
 * registered on the i18n instance is listed, so `zh` (and any others) appear
 * automatically once their strings are authored.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Languages } from 'lucide-vue-next'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'

const { t, locale, availableLocales } = useI18n({ useScope: 'global' })

/** Human-readable names for known locales; unknown codes fall back to the code. */
const LABELS: Record<string, string> = { en: 'English', zh: '中文' }
const options = computed(() =>
  availableLocales.map((code) => ({ value: code, label: LABELS[code] ?? code })),
)
</script>

<template>
  <Select v-model="locale">
    <SelectTrigger class="h-8 w-fit gap-1.5" :aria-label="t('common.language')" data-test="locale-trigger">
      <Languages class="size-4 text-muted" aria-hidden="true" />
      <SelectValue />
    </SelectTrigger>
    <SelectContent align="start">
      <SelectItem v-for="opt in options" :key="opt.value" :value="opt.value">
        {{ opt.label }}
      </SelectItem>
    </SelectContent>
  </Select>
</template>
