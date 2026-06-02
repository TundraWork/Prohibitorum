<script setup lang="ts">
import { useSessionStore } from '../stores/session'
const session = useSessionStore()

const userLinks = [
  { to: '/', label: 'Profile', icon: 'i-lucide-user' },
  { to: '/security', label: 'Security', icon: 'i-lucide-shield' },
  { to: '/sessions', label: 'Sessions', icon: 'i-lucide-monitor' },
  { to: '/connected', label: 'Connected accounts', icon: 'i-lucide-link' },
  { to: '/devices', label: 'Devices', icon: 'i-lucide-smartphone' },
]
const adminLinks = [
  { to: '/admin/accounts', label: 'Accounts', icon: 'i-lucide-users' },
  { to: '/admin/invitations', label: 'Invitations', icon: 'i-lucide-mail-plus' },
]
const plannedLinks = [
  { to: '/admin/oidc-clients', label: 'OIDC clients' },
  { to: '/admin/saml-providers', label: 'SAML providers' },
  { to: '/admin/signing-keys', label: 'Signing keys' },
  { to: '/admin/audit', label: 'Audit log' },
  { to: '/admin/settings', label: 'Settings' },
]
</script>

<template>
  <nav class="flex flex-col gap-1 p-3 w-56 shrink-0 border-r border-default min-h-screen">
    <RouterLink v-for="l in userLinks" :key="l.to" :to="l.to"
      class="flex items-center gap-2 px-3 py-2 rounded text-sm hover:bg-elevated"
      active-class="bg-elevated font-medium" exact-active-class="bg-elevated font-medium">
      <UIcon :name="l.icon" class="size-4" />{{ l.label }}
    </RouterLink>

    <template v-if="session.isAdmin">
      <div class="mt-4 mb-1 px-3 text-xs uppercase tracking-wide text-muted">Admin</div>
      <RouterLink v-for="l in adminLinks" :key="l.to" :to="l.to"
        class="flex items-center gap-2 px-3 py-2 rounded text-sm hover:bg-elevated"
        active-class="bg-elevated font-medium">
        <UIcon :name="l.icon" class="size-4" />{{ l.label }}
      </RouterLink>
      <div class="mt-3 mb-1 px-3 text-xs uppercase tracking-wide text-muted/60">Planned</div>
      <RouterLink v-for="l in plannedLinks" :key="l.to" :to="l.to"
        class="flex items-center gap-2 px-3 py-2 rounded text-sm text-muted/60 hover:bg-elevated"
        active-class="bg-elevated">
        <UIcon name="i-lucide-circle-dashed" class="size-4" />{{ l.label }}
      </RouterLink>
    </template>
  </nav>
</template>
