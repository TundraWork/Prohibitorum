<script setup lang="ts">
/**
 * ThemeToggle — a Light / System / Dark radiogroup bound to useTheme.
 * The three options are mutually exclusive, so this is a role="radiogroup"
 * of role="radio" buttons with a roving tabindex and Left/Right/Up/Down arrow
 * navigation (keyboard-first, WCAG 2.2 AA). Active state binds to `stored`
 * (the persisted selection, which can be 'auto'), not the resolved `mode`.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Sun, Moon, Monitor } from 'lucide-vue-next'
import { useTheme, type ThemeMode } from '@/composables/useTheme'
import { cn } from '@/lib/utils'

const { t } = useI18n()
const { stored, setMode } = useTheme()

const options = computed<{ value: ThemeMode; icon: typeof Sun; label: string; test: string }[]>(() => [
  { value: 'light', icon: Sun, label: t('theme.light'), test: 'theme-light' },
  { value: 'auto', icon: Monitor, label: t('theme.system'), test: 'theme-system' },
  { value: 'dark', icon: Moon, label: t('theme.dark'), test: 'theme-dark' },
])

function move(e: KeyboardEvent, delta: number): void {
  const opts = options.value
  const current = opts.findIndex((o) => o.value === stored.value)
  const next = ((current === -1 ? 0 : current) + delta + opts.length) % opts.length
  setMode(opts[next].value)
  const group = e.currentTarget as HTMLElement
  group.querySelectorAll<HTMLButtonElement>('[role="radio"]')[next]?.focus()
}
</script>

<template>
  <!-- Compact, label-less radiogroup sized for the sidebar footer. The per-button
       aria-label + the group aria-label carry the accessible naming (no visible
       text label needed beside three self-evident icons). Track uses the sidebar
       tone + border so the active bg-card pill reads against the sunken footer. -->
  <div
    role="radiogroup"
    :aria-label="t('theme.label')"
    class="inline-flex rounded-md border border-sidebar-border bg-sidebar p-0.5"
    @keydown.right.prevent="move($event, 1)"
    @keydown.down.prevent="move($event, 1)"
    @keydown.left.prevent="move($event, -1)"
    @keydown.up.prevent="move($event, -1)"
  >
    <button
      v-for="o in options"
      :key="o.value"
      type="button"
      role="radio"
      :data-test="o.test"
      :aria-label="o.label"
      :aria-checked="stored === o.value"
      :tabindex="stored === o.value ? 0 : -1"
      :class="cn(
        'inline-flex size-7 cursor-pointer items-center justify-center rounded-[7px] outline-none transition-colors',
        'focus-visible:ring-2 focus-visible:ring-ring',
        stored === o.value ? 'bg-card text-ink shadow-raised' : 'text-muted hover:text-ink',
      )"
      @click="setMode(o.value)"
    >
      <component :is="o.icon" class="size-4" aria-hidden="true" />
    </button>
  </div>
</template>
