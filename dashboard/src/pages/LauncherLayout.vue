<script setup lang="ts">
import { RouterView, RouterLink } from 'vue-router'
import { ShieldCheck } from 'lucide-vue-next'
import { useBrandingStore } from '@/stores/branding'
import NavUser from '@/components/custom/NavUser.vue'
import LocaleSwitcher from '@/components/custom/LocaleSwitcher.vue'
import ThemeToggle from '@/components/custom/ThemeToggle.vue'
// Decorative corner backdrop — JWST "Cosmic Cliffs" in the Carina Nebula
// (NASA/ESA/CSA, public domain). Purely ornamental; faded + masked so content
// stays readable, dimmed further in dark mode.
import backdrop from '@/assets/launcher-backdrop.webp'

const branding = useBrandingStore()
</script>

<template>
  <div class="relative min-h-screen overflow-hidden bg-canvas text-ink">
    <img
      :src="backdrop"
      alt=""
      aria-hidden="true"
      class="pointer-events-none absolute -left-28 -top-28 z-0 size-[32rem] select-none object-cover opacity-55 [mask-image:radial-gradient(circle_at_top_left,black,transparent_72%)] dark:opacity-35"
    />
    <header class="relative z-10 flex items-center justify-between border-b border-line/70 px-4 py-3 backdrop-blur-sm sm:px-6">
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
    <main class="relative z-10 mx-auto w-full max-w-5xl px-4 py-8 sm:px-6">
      <RouterView />
    </main>
  </div>
</template>
