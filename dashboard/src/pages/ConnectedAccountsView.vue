<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref, onMounted, computed } from 'vue'
import { api } from '../lib/api'
import { withSudo, ensureSudo } from '../lib/sudo'

interface Identity { id: number; idpSlug: string; idpDisplayName: string; upstreamEmail?: string | null; linkedAt: string }
interface Provider { slug: string; displayName: string }

const idents = ref<Identity[]>([]); const providers = ref<Provider[]>([])
const error = ref(''); const busy = ref(false); const armed = ref<number | null>(null)

function show(e: any) { error.value = e?.message ?? 'Something went wrong' }
async function load() {
  error.value = ''
  try { idents.value = await api.get<Identity[]>('/api/prohibitorum/me/identities') } catch (e) { show(e) }
  try { providers.value = await api.get<Provider[]>('/api/prohibitorum/auth/federation') } catch { /* providers optional */ }
}
const linkedSlugs = computed(() => new Set(idents.value.map(i => i.idpSlug)))

async function unlink(id: number) {
  if (busy.value) return; busy.value = true; error.value = ''
  try { await withSudo(() => api.post(`/api/prohibitorum/me/identities/${id}/unlink`)); armed.value = null; await load() } catch (e) { show(e); armed.value = null } finally { busy.value = false }
}
async function link(slug: string) {
  // The begin endpoint is a sudo-gated 302 redirect; step up proactively, then navigate.
  const ok = await ensureSudo()
  if (!ok) return
  window.location.assign(`/api/prohibitorum/me/identities/link/${encodeURIComponent(slug)}/begin?return_to=${encodeURIComponent('/connected')}`)
}
onMounted(load)
</script>

<template>
  <div class="space-y-4 max-w-3xl">
    <h1 class="text-lg font-semibold">Connected accounts</h1>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>

    <table class="w-full text-sm border-collapse">
      <thead><tr class="text-left text-muted border-b border-default">
        <th class="py-2 pr-4">Provider</th><th class="py-2 pr-4">Email</th><th class="py-2 pr-4">Linked</th><th class="py-2 pr-4">Actions</th>
      </tr></thead>
      <tbody>
        <tr v-for="i in idents" :key="i.id" class="border-b border-default/50">
          <td class="py-2 pr-4">{{ i.idpDisplayName }}</td>
          <td class="py-2 pr-4">{{ i.upstreamEmail || '—' }}</td>
          <td class="py-2 pr-4">{{ new Date(i.linkedAt).toLocaleDateString() }}</td>
          <td class="py-2 pr-4">
            <div class="inline-flex items-center gap-1">
              <template v-if="armed === i.id">
                <UButton data-test="unlink" type="button" size="xs" color="error" :disabled="busy" @click="unlink(i.id)">Confirm</UButton>
                <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armed = null">Cancel</UButton>
              </template>
              <UButton v-else data-test="unlink" type="button" size="xs" color="error" variant="soft" @click="armed = i.id">Unlink</UButton>
            </div>
          </td>
        </tr>
        <tr v-if="!idents.length"><td colspan="4" class="py-3 text-muted">No connected accounts.</td></tr>
      </tbody>
    </table>

    <div v-if="providers.length" class="space-y-2">
      <h2 class="text-sm font-medium">Link a provider</h2>
      <div class="flex flex-wrap gap-2">
        <UButton v-for="p in providers" :key="p.slug" type="button" size="sm" variant="soft"
          :disabled="linkedSlugs.has(p.slug)" @click="link(p.slug)">
          {{ p.displayName }}{{ linkedSlugs.has(p.slug) ? ' (linked)' : '' }}
        </UButton>
      </div>
      <p class="text-xs text-muted">Linking redirects to the provider; it completes only with a live upstream.</p>
    </div>
  </div>
</template>
