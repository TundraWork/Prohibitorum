<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LocaleSwitcher from './components/LocaleSwitcher.vue'

const { t } = useI18n()
</script>

<template>
  <UApp>
    <div class="min-h-screen flex flex-col items-center justify-center gap-6 p-4 bg-default">
      <header class="w-full max-w-md flex items-center justify-between">
        <span class="text-lg font-semibold">{{ t('app.name') }}</span>
        <LocaleSwitcher />
      </header>
      <main class="w-full max-w-md">
        <RouterView />
      </main>
    </div>
  </UApp>
</template>
