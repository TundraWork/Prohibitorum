<script setup lang="ts">
/**
 * LocaleMenuSection — the "Language" row + nested submenu for account menus
 * (PHB-7). Mirrors LocaleSwitcher's option logic (every registered locale,
 * human LABELS, code fallback) but as DropdownMenuSub menu items instead of a
 * Select, so the launcher header can collapse the control into the account
 * menu. Selection writes straight to the global locale ref.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Languages, Check } from 'lucide-vue-next'
import {
  DropdownMenuSub, DropdownMenuSubTrigger, DropdownMenuSubContent, DropdownMenuItem,
} from '@/components/ui/dropdown-menu'

const { t, locale, availableLocales } = useI18n({ useScope: 'global' })

/** Human-readable names for known locales; unknown codes fall back to the code. */
const LABELS: Record<string, string> = { en: 'English', zh: '中文' }
const options = computed(() =>
  availableLocales.map((code) => ({ value: code, label: LABELS[code] ?? code })),
)

function pick(code: string): void {
  locale.value = code
}
</script>

<template>
  <DropdownMenuSub>
    <DropdownMenuSubTrigger :aria-label="t('common.language')" data-test="locale-sub-trigger">
      <Languages class="size-4 text-muted" aria-hidden="true" />
      <span>{{ t('common.language') }}</span>
    </DropdownMenuSubTrigger>
    <DropdownMenuSubContent>
      <DropdownMenuItem
        v-for="opt in options"
        :key="opt.value"
        role="menuitemradio"
        :aria-checked="locale === opt.value"
        data-test="locale-option"
        class="cursor-pointer"
        @select="pick(opt.value)"
      >
        <span class="flex-1">{{ opt.label }}</span>
        <Check v-if="locale === opt.value" class="size-4" aria-hidden="true" />
      </DropdownMenuItem>
    </DropdownMenuSubContent>
  </DropdownMenuSub>
</template>
