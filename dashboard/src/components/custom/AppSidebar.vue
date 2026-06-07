<script setup lang="ts">
/**
 * AppSidebar — config-driven navigation over the vendored shadcn-vue Sidebar
 * primitive (the capability floor: collapse/drawer/tooltip/a11y come from it).
 * Header = brand mark (the single Ember moment). Content = Account nav group
 * (built links only for Spec 2a). Footer = identity + logout (utility tier).
 * An admin group renders only when auth.isAdmin (lands in Spec 3).
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ShieldCheck, User, MonitorSmartphone, LogOut, KeyRound, Link2, TabletSmartphone, Users, Ticket } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import {
  Sidebar, SidebarHeader, SidebarContent, SidebarFooter,
  SidebarGroup, SidebarGroupLabel, SidebarGroupContent,
  SidebarMenu, SidebarMenuItem, SidebarMenuButton,
} from '@/components/ui/sidebar'

const { t } = useI18n()
const auth = useAuthStore()
const route = useRoute()

const isActive = (to: string) =>
  to === '/' ? route.path === '/' : route.path === to || route.path.startsWith(to + '/')

const accountItems = computed(() => [
  { to: '/', label: t('nav.profile'), icon: User },
  { to: '/security', label: t('nav.security'), icon: KeyRound },
  { to: '/sessions', label: t('nav.sessions'), icon: MonitorSmartphone },
  { to: '/connected', label: t('nav.connected'), icon: Link2 },
  { to: '/devices', label: t('nav.devices'), icon: TabletSmartphone },
])

const adminItems = computed(() => [
  { to: '/admin/accounts', label: t('admin.nav.accounts'), icon: Users },
  { to: '/admin/invitations', label: t('admin.nav.invitations'), icon: Ticket },
])
</script>

<template>
  <Sidebar>
    <SidebarHeader>
      <div class="flex items-center gap-2.5 px-2 py-1.5">
        <span class="inline-flex size-8 items-center justify-center rounded-md bg-ember/12 text-ember ring-1 ring-inset ring-ember/15">
          <ShieldCheck class="size-5" aria-hidden="true" />
        </span>
        <span class="text-base font-semibold tracking-tight text-ink">Prohibitorum</span>
      </div>
    </SidebarHeader>

    <SidebarContent>
      <SidebarGroup>
        <SidebarGroupLabel>{{ t('nav.account') }}</SidebarGroupLabel>
        <SidebarGroupContent>
          <SidebarMenu>
            <SidebarMenuItem v-for="item in accountItems" :key="item.to">
              <SidebarMenuButton as-child :tooltip="item.label" :is-active="isActive(item.to)">
                <RouterLink :to="item.to">
                  <component :is="item.icon" aria-hidden="true" />
                  <span>{{ item.label }}</span>
                </RouterLink>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarGroupContent>
      </SidebarGroup>
      <SidebarGroup v-if="auth.isAdmin">
        <SidebarGroupLabel>{{ t('admin.nav.title') }}</SidebarGroupLabel>
        <SidebarGroupContent>
          <SidebarMenu>
            <SidebarMenuItem v-for="item in adminItems" :key="item.to">
              <SidebarMenuButton as-child :tooltip="item.label" :is-active="isActive(item.to)">
                <RouterLink :to="item.to">
                  <component :is="item.icon" aria-hidden="true" />
                  <span>{{ item.label }}</span>
                </RouterLink>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarGroupContent>
      </SidebarGroup>
    </SidebarContent>

    <SidebarFooter>
      <div class="flex flex-col gap-1 border-t border-sidebar-border pt-2">
        <div v-if="auth.me" class="flex min-w-0 flex-col px-2 py-1">
          <span class="truncate text-sm font-medium text-ink">{{ auth.me.displayName }}</span>
          <span class="truncate text-xs capitalize text-muted">{{ auth.me.role }}</span>
        </div>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton as-child :tooltip="t('nav.signOut')">
              <RouterLink to="/logout">
                <LogOut aria-hidden="true" />
                <span>{{ t('nav.signOut') }}</span>
              </RouterLink>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </div>
    </SidebarFooter>
  </Sidebar>
</template>
