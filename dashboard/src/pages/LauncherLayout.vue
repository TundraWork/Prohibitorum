<script setup lang="ts">
import { RouterView, RouterLink } from 'vue-router'
import { ShieldCheck } from 'lucide-vue-next'
import { useBrandingStore } from '@/stores/branding'
import NavUser from '@/components/custom/NavUser.vue'
import LocaleSwitcher from '@/components/custom/LocaleSwitcher.vue'
import ThemeToggle from '@/components/custom/ThemeToggle.vue'
// Decorative top-left corner backdrop. Purely ornamental: anchored to the
// top-left of the content area (it begins below the title bar, not the page
// top), with a square (box) fade so it dissolves into the canvas toward the
// right/bottom while keeping content readable. Theme-matched: a daylight variant
// for light mode, a moonlit one for dark. The swap is driven by the `.dark`
// CLASS (see <style> below) — exactly like `.app-bg`/`.dark .app-bg` in
// main.css. We deliberately do NOT use Tailwind `dark:` utilities here: this
// project has no `@custom-variant dark`, so `dark:*` compiles to a
// `prefers-color-scheme` media query and would track the OS instead of the app's
// theme toggle. The two image URLs are handed to CSS as custom properties.
import backdropLight from '@/assets/launcher-backdrop-light.webp'
import backdropDark from '@/assets/launcher-backdrop-dark.webp'

const branding = useBrandingStore()
</script>

<template>
  <div class="flex min-h-screen flex-col bg-canvas text-ink">
    <header class="relative z-10 flex items-center justify-between border-b border-line/70 px-4 py-3 sm:px-6">
      <RouterLink to="/" class="flex items-center gap-2 font-semibold hover:opacity-80 transition-opacity">
        <span class="inline-flex size-8 items-center justify-center overflow-hidden rounded-md bg-ember/12 text-ember ring-1 ring-inset ring-ember/15">
          <img v-if="branding.hasCustomIcon" :src="branding.iconSrc" :alt="branding.instanceName" class="size-full object-cover" />
          <ShieldCheck v-else class="size-5" aria-hidden="true" />
        </span>
        <span class="text-base tracking-tight text-ink">{{ branding.instanceName }}</span>
      </RouterLink>
      <div class="flex items-center gap-2">
        <LocaleSwitcher />
        <ThemeToggle />
        <NavUser variant="topbar" />
      </div>
    </header>
    <div class="relative flex-1 overflow-hidden">
      <div
        class="launcher-backdrop"
        aria-hidden="true"
        :style="{ '--backdrop-light': `url(${backdropLight})`, '--backdrop-dark': `url(${backdropDark})` }"
      />
      <main class="relative z-10 mx-auto w-full max-w-5xl px-4 py-8 sm:px-6">
        <RouterView />
      </main>
    </div>
  </div>
</template>

<style scoped>
/* Decorative corner backdrop — see the script comment. Theme swap is class-based
   (.dark) to match the app toggle; the fade is SQUARE (box-shaped contours), made
   by intersecting a horizontal and a vertical linear gradient rather than a
   single radial (which would fade in an ellipse). */
.launcher-backdrop {
  position: absolute;
  inset: 0 auto auto 0; /* top-left of the content area (below the header) */
  z-index: 0;
  height: 40rem;
  width: 30rem;
  max-width: 80vw;
  pointer-events: none;
  user-select: none;
  background-image: var(--backdrop-light);
  background-size: cover;
  background-position: left top;
  opacity: 0.85;

  --backdrop-fade:
    linear-gradient(to right, #000 58%, transparent 92%),
    linear-gradient(to bottom, #000 70%, transparent 96%);
  -webkit-mask-image: var(--backdrop-fade);
  mask-image: var(--backdrop-fade);
  -webkit-mask-composite: source-in; /* legacy WebKit keyword for "intersect" */
  mask-composite: intersect;
}

.dark .launcher-backdrop {
  background-image: var(--backdrop-dark);
  opacity: 0.9;
}
</style>
