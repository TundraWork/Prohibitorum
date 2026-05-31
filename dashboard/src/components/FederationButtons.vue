<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'

interface FederationProvider {
  slug: string
  displayName: string
}

// relativeReturnTo is the path+query of the (same-origin-guarded) return target.
// Federation requires a RELATIVE return_to.
const props = defineProps<{ relativeReturnTo: string }>()
const { t } = useI18n()

const providers = ref<FederationProvider[]>([])

onMounted(async () => {
  try {
    providers.value = await api.get<FederationProvider[]>('/api/prohibitorum/auth/federation')
  } catch {
    // No providers / fetch error: render nothing, keep other methods usable.
    providers.value = []
  }
})

function go(slug: string) {
  window.location.href =
    '/api/prohibitorum/auth/federation/' +
    encodeURIComponent(slug) +
    '/login?return_to=' +
    encodeURIComponent(props.relativeReturnTo)
}
</script>

<template>
  <div v-if="providers.length" class="flex flex-col gap-2">
    <UButton
      v-for="p in providers"
      :key="p.slug"
      block
      color="neutral"
      variant="subtle"
      @click="go(p.slug)"
    >
      {{ t('login.signInWith', { name: p.displayName }) }}
    </UButton>
  </div>
</template>
