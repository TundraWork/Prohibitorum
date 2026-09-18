<script setup lang="ts">
/**
 * Toaster — fixed overlay that renders the global toast queue.
 *
 * Toasts are owned by the framework-agnostic reactive singleton in
 * src/lib/toast.ts (so they can be pushed from api.ts / main.ts outside the
 * component tree). Mounted once in App.vue as a sibling to <RouterView/>.
 *
 * Variant accent lives on the icon only — the card surface stays neutral
 * (bg-card) for legibility, matching the dialog/overlay surface tokens. Error
 * toasts carry role="alert" so assistive tech announces them assertively;
 * info/success rely on the container's polite live region.
 */
import { X, CircleAlert, Info, CircleCheck } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { toasts, dismissToast } from '@/lib/toast'

const { t } = useI18n()
</script>

<template>
  <div
    class="fixed bottom-4 left-4 right-4 z-[100] flex w-auto flex-col gap-2 sm:left-auto sm:w-full sm:max-w-sm"
    aria-live="polite"
    aria-atomic="false"
  >
    <TransitionGroup
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="translate-y-2 opacity-0"
      enter-to-class="translate-y-0 opacity-100"
      leave-active-class="transition duration-150 ease-in"
      leave-from-class="translate-y-0 opacity-100"
      leave-to-class="translate-y-2 opacity-0"
    >
      <div
        v-for="toast in toasts"
        :key="toast.id"
        :role="toast.variant === 'error' ? 'alert' : undefined"
        class="flex items-start gap-3 rounded-lg border border-border bg-card px-4 py-3 shadow-[var(--shadow-overlay)]"
      >
        <CircleAlert
          v-if="toast.variant === 'error'"
          class="mt-0.5 size-4 shrink-0 text-destructive"
          aria-hidden="true"
        />
        <Info
          v-else-if="toast.variant === 'info'"
          class="mt-0.5 size-4 shrink-0 text-tide"
          aria-hidden="true"
        />
        <CircleCheck
          v-else
          class="mt-0.5 size-4 shrink-0 text-sage-700"
          aria-hidden="true"
        />

        <div class="min-w-0 flex-1">
          <p v-if="toast.title" class="text-sm font-medium text-ink">{{ toast.title }}</p>
          <p class="text-sm text-muted">{{ toast.message }}</p>
        </div>

        <button
          type="button"
          class="-mr-1 shrink-0 rounded p-0.5 text-muted hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          :aria-label="t('common.dismiss')"
          @click="dismissToast(toast.id)"
        >
          <X class="size-4" aria-hidden="true" />
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>
