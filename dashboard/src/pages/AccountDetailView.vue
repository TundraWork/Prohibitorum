<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../lib/api'
import CopyableUrl from '../components/CopyableUrl.vue'
import StatusBadge from '../components/StatusBadge.vue'

interface AccountView { id: number; username: string; displayName: string; role: string; disabled: boolean; createdAt: string; updatedAt: string; lastSignInAt?: string | null }
const route = useRoute(); const router = useRouter()
const id = computed(() => Number(route.params.id))
const acct = ref<AccountView | null>(null)
const role = ref('user'); const disabled = ref(false)
const error = ref(''); const busy = ref(false); const done = ref('')
const reissued = ref(''); const revoked = ref<number | null>(null); const armedDelete = ref(false)

function show(e: any) { error.value = e?.message ?? 'Something went wrong' }
async function load() {
  error.value = ''
  try { const a = await api.get<AccountView>(`/api/prohibitorum/accounts/${id.value}`); acct.value = a; role.value = a.role; disabled.value = a.disabled } catch (e) { show(e) }
}
async function save() {
  if (busy.value || !acct.value) return; busy.value = true; error.value = ''; done.value = ''
  try { await api.put(`/api/prohibitorum/accounts/${id.value}`, { displayName: acct.value.displayName, role: role.value, disabled: disabled.value }); done.value = 'Saved.'; await load() } catch (e) { show(e) } finally { busy.value = false }
}
async function revokeSessions() {
  if (busy.value) return; busy.value = true; error.value = ''
  try { const r = await api.post<{ revoked: number }>('/api/prohibitorum/accounts/revoke-sessions', { id: id.value }); revoked.value = r.revoked } catch (e) { show(e) } finally { busy.value = false }
}
async function reissue() {
  if (busy.value) return; busy.value = true; error.value = ''; reissued.value = ''
  try { const r = await api.post<{ url: string }>('/api/prohibitorum/accounts/reissue-enrollment', { id: id.value }); reissued.value = r.url } catch (e) { show(e) } finally { busy.value = false }
}
async function del() {
  if (busy.value) return; busy.value = true; error.value = ''
  try { await api.post('/api/prohibitorum/accounts/delete', { id: id.value }); router.push('/admin/accounts') } catch (e) { show(e); armedDelete.value = false } finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <div class="space-y-4 max-w-2xl">
    <RouterLink to="/admin/accounts" class="text-sm text-primary hover:underline">← Accounts</RouterLink>
    <h1 class="text-lg font-semibold">Account</h1>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>

    <UCard v-if="acct">
      <dl class="grid grid-cols-3 gap-y-2 text-sm">
        <dt class="text-muted">Username</dt><dd class="col-span-2 font-mono">{{ acct.username }}</dd>
        <dt class="text-muted">Display name</dt><dd class="col-span-2">{{ acct.displayName }}</dd>
        <dt class="text-muted">Role</dt>
        <dd class="col-span-2">
          <select v-model="role" class="w-40 border rounded px-2 py-1 text-sm">
            <option value="user">user</option>
            <option value="admin">admin</option>
          </select>
        </dd>
        <dt class="text-muted">Disabled</dt>
        <dd class="col-span-2"><input type="checkbox" v-model="disabled" /></dd>
      </dl>
      <template #footer>
        <div class="flex items-center gap-2">
          <UButton data-test="save" type="button" size="sm" :loading="busy" :disabled="busy" @click="save">Save</UButton>
          <span v-if="done" class="text-success text-sm">{{ done }}</span>
        </div>
      </template>
    </UCard>

    <UCard v-if="acct">
      <template #header><h2 class="font-medium">Sessions &amp; enrollment</h2></template>
      <div class="flex flex-wrap items-center gap-2">
        <UButton data-test="revoke-sessions" type="button" size="sm" variant="soft" :disabled="busy" @click="revokeSessions">Revoke all sessions</UButton>
        <span v-if="revoked !== null" class="text-sm text-muted">Revoked {{ revoked }} session(s).</span>
        <UButton type="button" size="sm" variant="soft" :disabled="busy" @click="reissue">Reissue enrollment</UButton>
      </div>
      <div v-if="reissued" class="mt-2"><CopyableUrl :url="reissued" /></div>
    </UCard>

    <UCard v-if="acct">
      <template #header>
        <div class="flex items-center gap-2"><h2 class="font-medium">Credentials</h2><StatusBadge kind="stub" /></div>
      </template>
      <p class="text-sm text-muted">Force-revoke a specific credential needs an admin "list account credentials" API (not yet implemented). <code>POST /accounts/credentials/delete {accountId, credentialId}</code> exists but there is no way to enumerate IDs yet. TODO(backend): add a list endpoint.</p>
    </UCard>

    <UCard v-if="acct">
      <template #header><h2 class="font-medium text-error">Danger zone</h2></template>
      <div class="inline-flex items-center gap-1">
        <template v-if="armedDelete">
          <UButton type="button" size="xs" color="error" :disabled="busy" @click="del">Confirm delete</UButton>
          <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armedDelete = false">Cancel</UButton>
        </template>
        <UButton v-else type="button" size="xs" color="error" variant="soft" @click="armedDelete = true">Delete account</UButton>
      </div>
    </UCard>
  </div>
</template>
