<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref, onMounted } from 'vue'
import { api } from '../../lib/api'
import { passkeyAddCredential, type CredentialView } from '../../lib/webauthn'

const rows = ref<CredentialView[]>([])
const error = ref(''); const busy = ref(false)
const armed = ref<number | null>(null); const editing = ref<number | null>(null); const draft = ref('')

function show(e: any) { error.value = e?.message ?? 'Something went wrong' }
async function load() { error.value = ''; try { rows.value = await api.get<CredentialView[]>('/api/prohibitorum/me/credentials') } catch (e) { show(e) } }

async function add() {
  if (busy.value) return; busy.value = true; error.value = ''
  try { await passkeyAddCredential(); await load() } catch (e) { show(e) } finally { busy.value = false }
}
function startRename(c: CredentialView) { editing.value = c.id; draft.value = c.nickname ?? '' }
async function saveRename(id: number) {
  if (busy.value) return; busy.value = true; error.value = ''
  try { await api.post('/api/prohibitorum/me/credentials/rename', { id, nickname: draft.value || null }); editing.value = null; await load() } catch (e) { show(e) } finally { busy.value = false }
}
async function del(id: number) {
  if (busy.value) return; busy.value = true; error.value = ''
  try { await api.post('/api/prohibitorum/me/credentials/delete', { id }); armed.value = null; await load() } catch (e) { show(e); armed.value = null } finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex items-center justify-between">
        <h2 class="font-medium">Passkeys</h2>
        <UButton data-test="add-passkey" type="button" size="xs" :loading="busy" :disabled="busy" @click="add">Add passkey</UButton>
      </div>
    </template>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm mb-2">{{ error }}</p>
    <table class="w-full text-sm border-collapse">
      <thead><tr class="text-left text-muted border-b border-default">
        <th class="py-2 pr-4">Nickname</th><th class="py-2 pr-4">ID</th><th class="py-2 pr-4">Added</th><th class="py-2 pr-4">Actions</th>
      </tr></thead>
      <tbody>
        <tr v-for="c in rows" :key="c.id" class="border-b border-default/50">
          <td class="py-2 pr-4">
            <UInput v-if="editing === c.id" v-model="draft" size="xs" placeholder="New nickname" />
            <template v-else>{{ c.nickname || '(unnamed)' }}</template>
          </td>
          <td class="py-2 pr-4 font-mono text-xs">…{{ c.credentialIdSuffix }}</td>
          <td class="py-2 pr-4">{{ c.createdAt ? new Date(c.createdAt).toLocaleDateString() : '—' }}</td>
          <td class="py-2 pr-4">
            <div class="inline-flex items-center gap-1">
              <template v-if="editing === c.id">
                <UButton type="button" size="xs" :disabled="busy" @click="saveRename(c.id)">Save</UButton>
                <UButton type="button" size="xs" color="neutral" variant="ghost" @click="editing = null">Cancel</UButton>
              </template>
              <template v-else>
                <UButton type="button" size="xs" variant="soft" @click="startRename(c)">Rename</UButton>
                <template v-if="armed === c.id">
                  <UButton data-test="del" type="button" size="xs" color="error" :disabled="busy" @click="del(c.id)">Confirm</UButton>
                  <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armed = null">Cancel</UButton>
                </template>
                <UButton v-else data-test="del" type="button" size="xs" color="error" variant="soft" @click="armed = c.id">Delete</UButton>
              </template>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </UCard>
</template>
