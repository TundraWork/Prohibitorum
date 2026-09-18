<script setup lang="ts">
/**
 * ThemeMenuSection — the "Theme" row + nested submenu for account menus
 * (PHB-7). Bound to useTheme's `stored` (the persisted selection, which can be
 * 'auto') so the checked mark tracks what the user picked, not the resolved
 * mode; picking an option calls setMode. Same three options as the sidebar
 * ThemeToggle radiogroup, expressed in menu-item radio semantics instead.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Sun, Moon, Monitor, Check } from 'lucide-vue-next'
import { useTheme, type ThemeMode } from '@/composables/useTheme'
import {
  DropdownMenuSub, DropdownMenuSubTrigger, DropdownMenuSubContent, DropdownMenuItem,
} from '@/components/ui/dropdown-menu'

const { t } = useI18n()
const { stored, setMode } = useTheme()

const options = computed<{ value: ThemeMode; icon: typeof Sun; label: string; test: string }[]>(() => [
  { value: 'light', icon: Sun, label: t('theme.light'), test: 'theme-light' },
  { value: 'auto', icon: Monitor, label: t('theme.system'), test: 'theme-system' },
  { value: 'dark', icon: Moon, label: t('theme.dark'), test: 'theme-dark' },
])
</script>

<template>
  <DropdownMenuSub>
    <DropdownMenuSubTrigger :aria-label="t('theme.label')" data-test="theme-sub-trigger">
      <Sun class="size-4 text-muted" aria-hidden="true" />
      <span>{{ t('theme.label') }}</span>
    </DropdownMenuSubTrigger>
    <DropdownMenuSubContent>
      <DropdownMenuItem
        v-for="o in options"
        :key="o.value"
        role="menuitemradio"
        :aria-checked="stored === o.value"
        :data-test="o.test"
        class="cursor-pointer"
        @select="setMode(o.value)"
      >
        <component :is="o.icon" class="size-4 text-muted" aria-hidden="true" />
        <span class="flex-1">{{ o.label }}</span>
        <Check v-if="stored === o.value" class="size-4" aria-hidden="true" />
      </DropdownMenuItem>
    </DropdownMenuSubContent>
  </DropdownMenuSub>
</template>
