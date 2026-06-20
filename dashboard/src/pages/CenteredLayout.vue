<script setup lang="ts">
/**
 * CenteredLayout — shared chrome for the threshold (unauthenticated) pages.
 *
 * Composition: the Drenched <AuthBackdrop/> behind a single centered, opaque
 * Surface card (lg radius, Overlay shadow — it floats over the scene). The
 * card's own surface guarantees WCAG 2.2 AA for its contents over any image;
 * the backdrop's scrim covers the corner chrome.
 *
 * Ember appears exactly once here — the brand mark (Scarce Accent Rule,
 * DESIGN.md §). The rest of the surface stays in the restrained Tide/neutral
 * system.
 *
 * Usage: imported per page (Spec 1 keeps it simple — no nested router layout):
 *   <CenteredLayout><!-- page body --></CenteredLayout>
 * Slots:
 *   default        — the page body (form, scopes, message…)
 *   #title         — optional card heading (falls back to the brand wordmark)
 *   #footer        — optional below-card area
 */
import { ShieldCheck } from 'lucide-vue-next'
import AuthBackdrop from '@/components/custom/AuthBackdrop.vue'
import LocaleSwitcher from '@/components/custom/LocaleSwitcher.vue'
import { Card } from '@/components/ui/card'
import { useBrandingStore } from '@/stores/branding'
const branding = useBrandingStore()
</script>

<template>
  <div class="relative min-h-screen w-full">
    <AuthBackdrop />

    <!-- Corner chrome — legible via the switcher's own surface chip + the scrim. -->
    <div class="absolute right-4 top-4 z-20">
      <LocaleSwitcher />
    </div>

    <main class="relative z-10 flex min-h-screen items-center justify-center p-4">
      <div class="flex w-full max-w-md flex-col items-center gap-6">
        <Card
          class="w-full gap-6 rounded-lg border-border px-6 py-8 shadow-none sm:px-8"
          :style="{ boxShadow: 'var(--shadow-overlay)' }"
        >
          <!-- Brand mark: the single Ember moment on the threshold. -->
          <header class="flex flex-col items-center gap-2 text-center">
            <span
              class="inline-flex size-10 items-center justify-center overflow-hidden rounded-md bg-ember/10 text-ember"
            >
              <img v-if="branding.hasCustomIcon" :src="branding.iconSrc" :alt="branding.instanceName" class="size-full object-cover" />
              <ShieldCheck v-else class="size-6" aria-hidden="true" />
            </span>
            <slot name="title">
              <span class="text-lg font-semibold tracking-tight text-ink">{{ branding.instanceName }}</span>
            </slot>
          </header>

          <slot />
        </Card>

        <div v-if="$slots.footer" class="text-center text-sm text-muted">
          <slot name="footer" />
        </div>
      </div>
    </main>
  </div>
</template>
