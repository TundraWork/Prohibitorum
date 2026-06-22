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
import { ShieldCheck, MonitorSmartphone, KeyRound, Link2, TabletSmartphone, Users, Ticket, AppWindow, Building2, Network, KeySquare, ScrollText, UsersRound, Settings, Waypoints } from 'lucide-vue-next'
import NavUser from '@/components/custom/NavUser.vue'
import LocaleSwitcher from '@/components/custom/LocaleSwitcher.vue'
import ThemeToggle from '@/components/custom/ThemeToggle.vue'
import { useAuthStore } from '@/stores/auth'
import { useBrandingStore } from '@/stores/branding'
import {
  Sidebar, SidebarHeader, SidebarContent, SidebarFooter,
  SidebarGroup, SidebarGroupLabel, SidebarGroupContent,
  SidebarMenu, SidebarMenuItem, SidebarMenuButton,
} from '@/components/ui/sidebar'

const { t } = useI18n()
const auth = useAuthStore()
const branding = useBrandingStore()
const route = useRoute()

const isActive = (to: string) =>
  to === '/' ? route.path === '/' : route.path === to || route.path.startsWith(to + '/')

const accountItems = computed(() => [
  { to: '/security', label: t('nav.security'), icon: KeyRound },
  { to: '/sessions', label: t('nav.sessions'), icon: MonitorSmartphone },
  { to: '/connected', label: t('nav.connected'), icon: Link2 },
  { to: '/devices', label: t('nav.devices'), icon: TabletSmartphone },
  { to: '/app-access', label: t('nav.appAccess'), icon: ShieldCheck },
])

const adminItems = computed(() => [
  { to: '/admin/accounts', label: t('admin.nav.accounts'), icon: Users },
  { to: '/admin/invitations', label: t('admin.nav.invitations'), icon: Ticket },
  { to: '/admin/groups', label: t('admin.nav.groups'), icon: UsersRound },
  { to: '/admin/signing-keys', label: t('admin.nav.signingKeys'), icon: KeySquare },
  { to: '/admin/audit', label: t('admin.nav.audit'), icon: ScrollText },
  { to: '/admin/settings', label: t('admin.nav.settings'), icon: Settings },
])

const federationItems = computed(() => [
  { to: '/admin/identity-providers', label: t('admin.nav.identityProviders'), icon: Network },
])

const applicationItems = computed(() => [
  { to: '/admin/oidc-applications', label: t('admin.nav.oidcApplications'), icon: AppWindow },
  { to: '/admin/saml-applications', label: t('admin.nav.samlApplications'), icon: Building2 },
  { to: '/admin/forward-auth-apps', label: t('admin.nav.forwardAuthApps'), icon: Waypoints },
])
</script>

<template>
  <Sidebar>
    <SidebarHeader>
      <RouterLink to="/" class="flex items-center gap-2.5 px-2 py-1.5 hover:opacity-80 transition-opacity">
        <span class="inline-flex size-8 items-center justify-center overflow-hidden rounded-md bg-ember/12 text-ember ring-1 ring-inset ring-ember/15">
          <img v-if="branding.hasCustomIcon" :src="branding.iconSrc" :alt="branding.instanceName" class="size-full object-cover" />
          <ShieldCheck v-else class="size-5" aria-hidden="true" />
        </span>
        <span class="text-base font-semibold tracking-tight text-ink">{{ branding.instanceName }}</span>
      </RouterLink>
    </SidebarHeader>

    <SidebarContent role="navigation" :aria-label="t('nav.primaryLabel')">
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
      <SidebarGroup v-if="auth.isAdmin">
        <SidebarGroupLabel>{{ t('admin.nav.federation') }}</SidebarGroupLabel>
        <SidebarGroupContent>
          <SidebarMenu>
            <SidebarMenuItem v-for="item in federationItems" :key="item.to">
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
        <SidebarGroupLabel>{{ t('admin.nav.applications') }}</SidebarGroupLabel>
        <SidebarGroupContent>
          <SidebarMenu>
            <SidebarMenuItem v-for="item in applicationItems" :key="item.to">
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

    <SidebarFooter class="border-t border-sidebar-border">
      <!-- Utility controls on one line: language (left) + theme (right). -->
      <div class="flex items-center justify-between gap-2 px-2 pt-0.5">
        <LocaleSwitcher />
        <ThemeToggle />
      </div>
      <NavUser />
    </SidebarFooter>
  </Sidebar>
</template>
