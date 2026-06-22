<script setup lang="ts">
import { RouterView, RouterLink } from 'vue-router'
import { ShieldCheck } from 'lucide-vue-next'
import { useBrandingStore } from '@/stores/branding'
import NavUser from '@/components/custom/NavUser.vue'
import LocaleSwitcher from '@/components/custom/LocaleSwitcher.vue'
import ThemeToggle from '@/components/custom/ThemeToggle.vue'

const branding = useBrandingStore()
</script>

<template>
  <div class="min-h-screen bg-canvas text-ink">
    <header class="flex items-center justify-between border-b border-line px-4 py-3 sm:px-6">
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
    <main class="mx-auto w-full max-w-5xl px-4 py-8 sm:px-6">
      <RouterView />
    </main>
  </div>
</template>
