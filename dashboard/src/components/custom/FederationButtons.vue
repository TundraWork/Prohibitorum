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
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useResource } from '@/composables/useResource'
import { federationQuery } from '@/queries/resources'
import { useReturnTo } from '@/composables/useReturnTo'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import AppIcon from '@/components/custom/AppIcon.vue'
import BrandButton from '@/components/custom/BrandButton.vue'
import { providerBrand } from '@/lib/providerBrand'

interface FederationProvider {
  slug: string
  displayName: string
  iconUrl?: string | null
  protocol?: string
}

const { t } = useI18n()
const { returnTo } = useReturnTo()

const query = useResource(federationQuery<FederationProvider[]>())
const providers = computed(() => query.data.value ?? [])
const loading = query.busy

function startFederation(slug: string): void {
  // Forward the client-sanitized returnTo (safeReturnTo): the federation login
  // handler is fail-CLOSED (validateFederationReturnTo → 400 on a bad value),
  // so pre-sanitizing here keeps a same-origin-absolute bounce from tripping
  // that error branch. (The login ceremony, by contrast, forwards the raw value
  // to its fail-soft handler.) Either way the server re-validates server-side.
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
    <p class="text-center text-sm text-muted">{{ t('login.federationHeading') }}</p>
    <div class="flex flex-col gap-2">
      <template v-for="p in providers" :key="p.slug">
        <BrandButton
          v-if="providerBrand(p.protocol)"
          :protocol="p.protocol!"
          :label="p.displayName"
          :data-test="`${p.protocol}-login`"
          @click="startFederation(p.slug)"
        />
        <Button
          v-else
          type="button"
          variant="outline"
          class="w-full justify-start gap-2"
          @click="startFederation(p.slug)"
        >
          <AppIcon :src="p.iconUrl" :name="p.displayName" size="sm" />
          <span>{{ p.displayName }}</span>
        </Button>
      </template>
    </div>
  </div>
</template>
