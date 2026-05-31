<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useSessionStore } from '../stores/session'

const { t } = useI18n()
const router = useRouter()
const session = useSessionStore()

function logout() {
  router.push('/logout')
}
</script>

<template>
  <UCard class="max-w-xl">
    <template #header>
      <h1 class="text-lg font-semibold">{{ t('profile.title') }}</h1>
    </template>
    <dl class="grid grid-cols-3 gap-y-3 text-sm">
      <dt class="text-muted">{{ t('profile.username') }}</dt>
      <dd class="col-span-2">{{ session.me?.username }}</dd>
      <dt class="text-muted">{{ t('profile.displayName') }}</dt>
      <dd class="col-span-2">{{ session.me?.displayName }}</dd>
      <dt class="text-muted">{{ t('profile.role') }}</dt>
      <dd class="col-span-2">{{ session.me?.role }}</dd>
    </dl>
    <template #footer>
      <UButton type="button" color="neutral" variant="soft" @click="logout">{{ t('profile.logout') }}</UButton>
    </template>
  </UCard>
</template>
