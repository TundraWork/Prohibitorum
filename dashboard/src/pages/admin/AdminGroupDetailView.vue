<script setup lang="ts">
/**
 * AdminGroupDetailView (/admin/groups/:id) — edit group fields + exposed toggle +
 * member management (add/remove). All mutations go through withSudo.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import SectionTitle from '@/components/custom/SectionTitle.vue'
import CardSkeleton from '@/components/custom/CardSkeleton.vue'
import BackLink from '@/components/custom/BackLink.vue'
import SettingRow from '@/components/custom/SettingRow.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import { UserMinus } from 'lucide-vue-next'

interface GroupView {
  id: number
  slug: string
  displayName: string
  description?: string
  exposedToDownstream: boolean
  memberCount: number
  createdAt: string
}

interface GroupMemberView {
  id: number
  username: string
  displayName: string
}

interface Account {
  id: number
  username: string
  displayName: string
  role: string
  disabled: boolean
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const groupId = Number(route.params.id)
const { busy, error, run, errorText } = useApi()
// Separate composable for member operations so errors surface independently
const memberApi = useApi()
// Separate composable for accounts list — must NOT share memberApi (busy-guard race in Promise.all)
const accountsApi = useApi()

const group = ref<GroupView | null>(null)
const notFound = ref(false)

const slug = ref('')
const displayName = ref('')
const description = ref('')
const exposedToDownstream = ref(false)
const slugError = ref('')
const { flag: saved, trigger: triggerSaved } = useTransientFlag()

const members = ref<GroupMemberView[]>([])
const allAccounts = ref<Account[]>([])
const selectedAccountId = ref<string>('')
const confirmRemoveId = ref<number | null>(null)
const confirmDeleteGroup = ref(false)

const SLUG_RE = /^[a-z0-9](-?[a-z0-9])*$/

function validateSlug(s: string): string {
  if (!s) return t('admin.groups.slugInvalid')
  if (!SLUG_RE.test(s)) return t('admin.groups.slugInvalid')
  return ''
}

function onSlugInput(): void {
  slugError.value = validateSlug(slug.value)
}

// Accounts not already in the group
const addableAccounts = computed(() => {
  const memberIds = new Set(members.value.map((m) => m.id))
  return allAccounts.value.filter((a) => !memberIds.has(a.id))
})

async function load(): Promise<void> {
  const g = await run(() => api.get<GroupView>(`/api/prohibitorum/groups/${groupId}`))
  if (!g) { if (error.value?.code === 'group_not_found') notFound.value = true; return }
  group.value = g
  slug.value = g.slug
  displayName.value = g.displayName
  description.value = g.description ?? ''
  exposedToDownstream.value = g.exposedToDownstream
}

async function loadMembers(): Promise<void> {
  const res = await memberApi.run(() => api.get<GroupMemberView[]>(`/api/prohibitorum/groups/${groupId}/members`))
  if (res) members.value = res
}

async function loadAccounts(): Promise<void> {
  const res = await accountsApi.run(() => api.get<Account[]>('/api/prohibitorum/accounts'))
  if (res) allAccounts.value = res
}

async function save(): Promise<void> {
  const err = validateSlug(slug.value)
  if (err) { slugError.value = err; return }
  slugError.value = ''
  const updated = await run(() => withSudo(() => api.put<GroupView>(`/api/prohibitorum/groups/${groupId}`, {
    slug: slug.value,
    displayName: displayName.value,
    description: description.value || undefined,
    exposedToDownstream: exposedToDownstream.value,
  }), t('sudo.reason.saveChanges')))
  if (updated) { group.value = updated; triggerSaved() }
}

async function addMember(): Promise<void> {
  if (!selectedAccountId.value) return
  const accountId = Number(selectedAccountId.value)
  const ok = await memberApi.run(() => withSudo(() => api.post<object>(`/api/prohibitorum/groups/${groupId}/members`, { accountId })))
  if (ok !== undefined) {
    selectedAccountId.value = ''
    await loadMembers()
  }
}

async function removeMember(): Promise<void> {
  if (confirmRemoveId.value === null) return
  const accountId = confirmRemoveId.value
  const ok = await memberApi.run(() => withSudo(() => api.post<object>(`/api/prohibitorum/groups/${groupId}/members/remove`, { accountId })))
  confirmRemoveId.value = null
  if (ok !== undefined) await loadMembers()
}

async function destroy(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/groups/delete', { id: groupId })
    return true as const
  }, t('sudo.reason.deleteApp')))
  confirmDeleteGroup.value = false
  if (ok) router.push('/admin/groups')
}

const memberToRemove = computed(() => members.value.find((m) => m.id === confirmRemoveId.value))

onMounted(async () => {
  await load()
  if (!notFound.value) {
    await Promise.all([loadMembers(), loadAccounts()])
  }
})
</script>
<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <BackLink to="/admin/groups" :label="t('admin.groups.back')" />
    <Alert v-if="errorText && !notFound" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <p v-if="notFound" class="text-sm text-muted" role="status">{{ t('admin.groups.notFound') }}</p>

    <CardSkeleton v-else-if="busy && !group" />

    <template v-else-if="group">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ group.displayName }}</h1>

      <!-- Config card -->
      <Card>
        <CardHeader><CardTitle>{{ t('admin.groups.configTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-1.5">
            <Label for="slug">{{ t('admin.groups.slug') }}</Label>
            <Input id="slug" name="slug" v-model="slug" data-test="slug" @input="onSlugInput" />
            <p v-if="slugError" class="text-xs text-destructive" role="alert">{{ slugError }}</p>
            <p class="text-xs text-muted">{{ t('admin.groups.slugChangeWarning') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.groups.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" data-test="displayName" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="description">{{ t('admin.groups.description') }}</Label>
            <Input id="description" name="description" v-model="description" data-test="description" />
          </div>
          <SettingRow :label="t('admin.groups.exposed')" :description="t('admin.groups.exposedHint')" for="exposed">
            <Switch id="exposed" v-model="exposedToDownstream" data-test="exposed" />
          </SettingRow>
          <div class="flex items-center gap-3">
            <Button type="button" :disabled="busy" data-test="save" @click="save">{{ t('admin.groups.save') }}</Button>
            <StatusMessage :show="saved">{{ t('admin.groups.saved') }}</StatusMessage>
          </div>
        </CardContent>
      </Card>

      <!-- Members card -->
      <Card>
        <CardHeader><CardTitle>{{ t('admin.groups.members') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <Alert v-if="memberApi.errorText.value" variant="destructive" role="alert" aria-live="polite">
            <AlertDescription>{{ memberApi.errorText.value }}</AlertDescription>
          </Alert>

          <!-- Add member row -->
          <div class="flex items-center gap-2">
            <Select v-model="selectedAccountId" data-test="member-select">
              <SelectTrigger class="flex-1">
                <SelectValue :placeholder="t('admin.groups.addMemberPlaceholder')" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="a in addableAccounts" :key="a.id" :value="String(a.id)">
                  {{ a.displayName }} ({{ a.username }})
                </SelectItem>
              </SelectContent>
            </Select>
            <Button type="button" :disabled="memberApi.busy.value || !selectedAccountId" data-test="add-member" @click="addMember">
              {{ t('admin.groups.addMember') }}
            </Button>
          </div>

          <!-- Member list -->
          <div v-if="members.length" class="flex flex-col gap-2">
            <div v-for="m in members" :key="m.id"
                 class="flex items-center justify-between rounded-md border border-border px-3 py-2"
                 :data-test="`member-row-${m.id}`">
              <div class="flex min-w-0 flex-col">
                <span class="truncate font-medium text-ink">{{ m.displayName }}</span>
                <span class="truncate text-xs text-muted">{{ m.username }}</span>
              </div>
              <Button type="button" variant="ghost" size="sm" class="text-muted hover:text-destructive"
                      :data-test="`remove-member-${m.id}`"
                      @click="confirmRemoveId = m.id">
                <UserMinus class="size-4" aria-hidden="true" />
                <span class="sr-only">{{ t('admin.groups.removeMember') }}</span>
              </Button>
            </div>
          </div>
          <EmptyState v-else :title="t('admin.groups.membersEmpty')" />
        </CardContent>
      </Card>

      <!-- Danger zone -->
      <Card class="border-destructive/30 bg-destructive/[0.02]">
        <CardHeader><CardTitle class="text-destructive">{{ t('admin.groups.dangerTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-2">
            <SectionTitle as="h4">{{ t('admin.groups.deleteTitle') }}</SectionTitle>
            <p class="text-xs text-muted">{{ t('admin.groups.deleteHelp') }}</p>
            <Button type="button" variant="destructive" class="w-fit" :disabled="busy" data-test="delete" @click="confirmDeleteGroup = true">
              {{ t('admin.groups.delete') }}
            </Button>
          </div>
        </CardContent>
      </Card>
    </template>

    <!-- Remove member confirm -->
    <ConfirmDialog
      :open="confirmRemoveId !== null"
      :title="t('admin.groups.removeMemberConfirmTitle')"
      :confirm-label="t('admin.groups.removeMember')"
      :busy="memberApi.busy.value"
      @update:open="(v) => { if (!v) confirmRemoveId = null }"
      @cancel="confirmRemoveId = null"
      @confirm="removeMember">
      {{ t('admin.groups.removeMemberConfirmBody', { name: memberToRemove?.displayName ?? '' }) }}
    </ConfirmDialog>

    <!-- Delete group confirm -->
    <ConfirmDialog
      :open="confirmDeleteGroup"
      :title="t('admin.groups.deleteConfirmTitle')"
      :confirm-label="t('admin.groups.delete')"
      :busy="busy"
      @update:open="(v) => { if (!v) confirmDeleteGroup = false }"
      @cancel="confirmDeleteGroup = false"
      @confirm="destroy">
      {{ t('admin.groups.deleteConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
