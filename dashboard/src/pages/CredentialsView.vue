<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'

interface CredentialView {
  id: number
  credentialIdSuffix: string
  nickname: string | null
  transports: string[]
  backupState: boolean
  attestationType: string
  createdAt: string
  lastUsedAt?: string | null
}

const { t, te } = useI18n()
const rows = ref<CredentialView[]>([])
const error = ref('')
const busy = ref(false)
const armed = ref<number | null>(null)
const editing = ref<number | null>(null)
const draft = ref('')

function show(e: any) {
  const code = e?.code as string | undefined
  error.value = code && te('errors.' + code) ? t('errors.' + code) : (e?.message ?? t('errors.server_error'))
}

async function load() {
  error.value = ''
  try {
    rows.value = await api.get<CredentialView[]>('/api/prohibitorum/me/credentials')
  } catch (e) { show(e) }
}

function startRename(c: CredentialView) {
  editing.value = c.id
  draft.value = c.nickname ?? ''
}

async function saveRename(id: number) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api.post('/api/prohibitorum/me/credentials/rename', { id, nickname: draft.value || null })
    editing.value = null
    await load()
  } catch (e) { show(e) } finally { busy.value = false }
}

async function del(id: number) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api.post('/api/prohibitorum/me/credentials/delete', { id })
    armed.value = null
    await load()
  } catch (e) { show(e); armed.value = null } finally { busy.value = false }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-lg font-semibold">{{ t('credentials.title') }}</h1>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>
    <table class="w-full text-sm border-collapse">
      <thead>
        <tr class="text-left text-muted border-b border-default">
          <th class="py-2 pr-4">{{ t('credentials.nickname') }}</th>
          <th class="py-2 pr-4">{{ t('credentials.suffix') }}</th>
          <th class="py-2 pr-4">{{ t('credentials.transports') }}</th>
          <th class="py-2 pr-4">{{ t('credentials.createdAt') }}</th>
          <th class="py-2 pr-4">{{ t('credentials.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="c in rows" :key="c.id" class="border-b border-default/50">
          <td class="py-2 pr-4">
            <template v-if="editing === c.id">
              <UInput v-model="draft" size="xs" :placeholder="t('credentials.renamePrompt')" />
            </template>
            <template v-else>
              {{ c.nickname || t('credentials.unnamed') }}
            </template>
          </td>
          <td class="py-2 pr-4 font-mono text-xs">…{{ c.credentialIdSuffix }}</td>
          <td class="py-2 pr-4 text-xs">{{ c.transports.join(', ') }}</td>
          <td class="py-2 pr-4">{{ new Date(c.createdAt).toLocaleDateString() }}</td>
          <td class="py-2 pr-4">
            <div class="inline-flex items-center gap-1">
              <template v-if="editing === c.id">
                <UButton type="button" size="xs" :disabled="busy" @click="saveRename(c.id)">{{ t('common.save') }}</UButton>
                <UButton type="button" size="xs" color="neutral" variant="ghost" @click="editing = null">{{ t('common.cancel') }}</UButton>
              </template>
              <template v-else>
                <UButton type="button" size="xs" variant="soft" @click="startRename(c)">{{ t('common.rename') }}</UButton>
                <template v-if="armed === c.id">
                  <UButton data-test="del" type="button" size="xs" color="error" :disabled="busy" @click="del(c.id)">{{ t('common.confirm') }}</UButton>
                  <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armed = null">{{ t('common.cancel') }}</UButton>
                </template>
                <UButton v-else data-test="del" type="button" size="xs" color="error" variant="soft" @click="armed = c.id">{{ t('common.delete') }}</UButton>
              </template>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
