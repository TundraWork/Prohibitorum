<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useSessionStore } from '../stores/session'
import AppSidebar from '../components/AppSidebar.vue'
import LocaleSwitcher from '../components/LocaleSwitcher.vue'

const { t } = useI18n()
const router = useRouter()
const session = useSessionStore()

function logout() {
  router.push('/logout')
}
</script>

<template>
  <div class="min-h-screen flex bg-default">
    <AppSidebar />
    <div class="flex-1 flex flex-col">
      <header class="flex items-center justify-between px-6 py-3 border-b border-default">
        <span class="text-lg font-semibold">{{ t('app.name') }}</span>
        <div class="flex items-center gap-3">
          <span class="text-sm text-muted">{{ session.me?.displayName }}</span>
          <LocaleSwitcher />
          <UButton type="button" size="sm" color="neutral" variant="ghost" @click="logout">
            {{ t('nav.logout') }}
          </UButton>
        </div>
      </header>
      <main class="flex-1 p-6">
        <RouterView />
      </main>
    </div>
  </div>
</template>
