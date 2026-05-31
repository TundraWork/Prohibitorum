<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useSessionStore } from '../stores/session'

const { t } = useI18n()
const session = useSessionStore()

const userLinks = [
  { to: '/', label: 'nav.profile', icon: 'i-lucide-user' },
  { to: '/sessions', label: 'nav.sessions', icon: 'i-lucide-monitor' },
  { to: '/credentials', label: 'nav.credentials', icon: 'i-lucide-key-round' },
]
const adminLinks = [
  { to: '/admin/accounts', label: 'nav.accounts', icon: 'i-lucide-users' },
  { to: '/admin/invitations', label: 'nav.invitations', icon: 'i-lucide-mail-plus' },
]
</script>

<template>
  <nav class="flex flex-col gap-1 p-3 w-56 shrink-0 border-r border-default min-h-screen">
    <RouterLink
      v-for="l in userLinks"
      :key="l.to"
      :to="l.to"
      class="flex items-center gap-2 px-3 py-2 rounded text-sm hover:bg-elevated"
      active-class="bg-elevated font-medium"
      exact-active-class="bg-elevated font-medium"
    >
      <UIcon :name="l.icon" class="size-4" />
      {{ t(l.label) }}
    </RouterLink>

    <template v-if="session.isAdmin">
      <div class="mt-4 mb-1 px-3 text-xs uppercase tracking-wide text-muted">{{ t('nav.admin') }}</div>
      <RouterLink
        v-for="l in adminLinks"
        :key="l.to"
        :to="l.to"
        class="flex items-center gap-2 px-3 py-2 rounded text-sm hover:bg-elevated"
        active-class="bg-elevated font-medium"
      >
        <UIcon :name="l.icon" class="size-4" />
        {{ t(l.label) }}
      </RouterLink>
    </template>
  </nav>
</template>
