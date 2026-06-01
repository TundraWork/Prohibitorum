<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LocaleSwitcher from '../components/LocaleSwitcher.vue'
import { isDevMode } from '../lib/devMode'

const { t } = useI18n()
const dev = isDevMode()
</script>

<template>
  <div class="min-h-screen flex flex-col items-center justify-center gap-6 p-4 bg-default">
    <header class="w-full max-w-md flex items-center justify-between">
      <span class="text-lg font-semibold">{{ t('app.name') }}</span>
      <div class="flex items-center gap-2">
        <RouterLink v-if="dev" to="/dev" class="text-xs text-muted hover:underline">🛠 dev</RouterLink>
        <LocaleSwitcher />
      </div>
    </header>
    <main class="w-full max-w-md">
      <RouterView />
    </main>
  </div>
</template>
