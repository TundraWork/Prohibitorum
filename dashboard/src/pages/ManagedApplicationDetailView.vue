<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import { keys, type SessionView } from '@/queries/resources'
import AdminOidcClientDetailView from './admin/AdminOidcClientDetailView.vue'
import AdminForwardAuthAppDetailView from './admin/AdminForwardAuthAppDetailView.vue'
import AdminSamlProviderDetailView from './admin/AdminSamlProviderDetailView.vue'

const route = useRoute()
const queryClient = useQueryClient()
const currentAccountId = computed(() => queryClient.getQueryData<SessionView>(keys.me)?.id)
const detail = computed(() => {
  switch (String(route.params.kind)) {
    case 'oidc': return AdminOidcClientDetailView
    case 'forward_auth': return AdminForwardAuthAppDetailView
    case 'saml': return AdminSamlProviderDetailView
    default: return null
  }
})
</script>

<template>
  <div data-test="managed-application-detail">
    <component
      :is="detail"
      v-if="detail"
      mode="manager"
      :current-account-id="currentAccountId"
    />
  </div>
</template>
