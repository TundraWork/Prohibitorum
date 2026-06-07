<script setup lang="ts">
/**
 * DashboardLayout — the authenticated shell. SidebarProvider keeps the
 * sidebar's collapse/drawer state across route changes; SidebarInset holds the
 * routed page. SudoModal is mounted ONCE here so any page's withSudo() can
 * drive it.
 */
import { onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { SidebarProvider, SidebarInset, SidebarTrigger } from '@/components/ui/sidebar'
import AppSidebar from '@/components/custom/AppSidebar.vue'
import SudoModal from '@/components/custom/SudoModal.vue'

const auth = useAuthStore()
onMounted(() => { void auth.ensureLoaded() })
</script>

<template>
  <SidebarProvider>
    <AppSidebar />
    <SidebarInset>
      <header class="flex h-14 items-center gap-2 border-b border-border px-4">
        <SidebarTrigger />
      </header>
      <!-- SidebarInset already renders the page's <main> landmark; this is a plain content wrapper. -->
      <div class="flex-1 p-6">
        <RouterView />
      </div>
    </SidebarInset>
    <SudoModal />
  </SidebarProvider>
</template>
