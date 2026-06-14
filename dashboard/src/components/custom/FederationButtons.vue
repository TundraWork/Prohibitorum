<script setup lang="ts">
/**
 * FederationButtons — lists the enabled upstream IdPs and starts a federated
 * login on click.
 *
 * Flow:
 *   GET  /api/prohibitorum/auth/federation            → [{slug, displayName}]
 *   click → full-page redirect to
 *   GET  /api/prohibitorum/auth/federation/{slug}/login?return_to=<guarded>
 *
 * The redirect is intentionally a full navigation (window.location.assign) so
 * the upstream OIDC dance owns the browser. return_to is the same-origin-guarded
 * value from useReturnTo, forwarded so the user lands back where they started.
 *
 * Renders nothing when no providers are configured (or if the list fails to
 * load — federation is an optional path, never a hard error on the login page).
 */
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useReturnTo } from '@/composables/useReturnTo'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import OrDivider from '@/components/custom/OrDivider.vue'

interface FederationProvider {
  slug: string
  displayName: string
}

const { t } = useI18n()
const { returnTo } = useReturnTo()

const providers = ref<FederationProvider[]>([])
const loading = ref(true)

onMounted(async () => {
  try {
    providers.value = await api.get<FederationProvider[]>('/api/prohibitorum/auth/federation')
  } catch {
    // Optional path — leave the list empty and render nothing.
    providers.value = []
  } finally {
    loading.value = false
  }
})

function startFederation(slug: string): void {
  const url =
    `/api/prohibitorum/auth/federation/${encodeURIComponent(slug)}/login` +
    `?return_to=${encodeURIComponent(returnTo.value)}`
  window.location.assign(url)
}
</script>

<template>
  <!-- While loading: show ghost placeholders so the layout doesn't jump -->
  <div v-if="loading" class="flex flex-col gap-2" role="status" aria-busy="true">
    <Skeleton class="h-9 w-full rounded-md" />
    <Skeleton class="h-9 w-full rounded-md" />
  </div>

  <div v-else-if="providers.length" class="flex flex-col gap-4">
    <OrDivider :label="t('login.orDivider')" />
    <p class="text-center text-sm text-muted">{{ t('login.federationHeading') }}</p>
    <div class="flex flex-col gap-2">
      <Button
        v-for="p in providers"
        :key="p.slug"
        type="button"
        variant="outline"
        class="w-full"
        @click="startFederation(p.slug)"
      >
        {{ p.displayName }}
      </Button>
    </div>
  </div>
</template>
