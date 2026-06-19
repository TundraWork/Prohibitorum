<script setup lang="ts">
/**
 * DashboardLayout — the authenticated shell. SidebarProvider keeps the
 * sidebar's collapse/drawer state across route changes; SidebarInset holds the
 * routed page. SudoModal is mounted ONCE here so any page's withSudo() can
 * drive it.
 */
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { SidebarProvider, SidebarInset, SidebarTrigger } from '@/components/ui/sidebar'
import AppSidebar from '@/components/custom/AppSidebar.vue'
import SudoModal from '@/components/custom/SudoModal.vue'

const auth = useAuthStore()
const route = useRoute()
const { t } = useI18n()

onMounted(() => { void auth.ensureLoaded() })

// Map route names to i18n keys for the sticky header orientation title.
// Detail routes fall back to their parent section key.
const ROUTE_TITLE_KEYS: Record<string, string> = {
  security: 'nav.security',
  sessions: 'nav.sessions',
  connected: 'nav.connected',
  devices: 'nav.devices',
  'admin-accounts': 'admin.nav.accounts',
  'admin-account-detail': 'admin.nav.accounts',
  'admin-invitations': 'admin.nav.invitations',
  'admin-oidc-applications': 'admin.nav.oidcApplications',
  'admin-oidc-application-detail': 'admin.nav.oidcApplications',
  'admin-saml-applications': 'admin.nav.samlApplications',
  'admin-saml-application-detail': 'admin.nav.samlApplications',
  'admin-identity-providers': 'admin.nav.identityProviders',
  'admin-identity-provider-detail': 'admin.nav.identityProviders',
  'admin-signing-keys': 'admin.nav.signingKeys',
  'admin-audit': 'admin.nav.audit',
}

const pageTitle = computed(() => {
  const name = String(route.name ?? '')
  const key = ROUTE_TITLE_KEYS[name]
  return key ? t(key) : ''
})

// Move focus to the page content on client-side navigation so keyboard and
// screen-reader users land at the new content rather than a stale control
// (WCAG 2.4.3). The watch is lazy, so it does not fire — or steal focus — on
// the initial load, only on subsequent route changes.
const contentRef = ref<HTMLElement | null>(null)
watch(() => route.path, () => { contentRef.value?.focus() })
</script>

<template>
  <SidebarProvider>
    <AppSidebar />
    <SidebarInset>
      <header class="sticky top-0 z-10 flex h-14 items-center gap-2 border-b border-border bg-background px-4">
        <SidebarTrigger />
        <!-- Orientation label, not a heading: each routed page renders its own <h1>. -->
        <p v-if="pageTitle" class="text-sm font-medium text-ink">{{ pageTitle }}</p>
      </header>
      <!-- SidebarInset already renders the page's <main> landmark; this is a plain content wrapper. -->
      <div ref="contentRef" tabindex="-1" class="flex-1 p-6 sm:p-8 outline-none">
        <RouterView />
      </div>
    </SidebarInset>
    <SudoModal />
  </SidebarProvider>
</template>
