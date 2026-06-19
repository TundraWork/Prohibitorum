<script setup lang="ts">
/**
 * AppAccessCard — reusable "App access" card for OIDC and SAML detail views.
 * Renders a restrict toggle (Switch) plus assigned groups and accounts with
 * add-pickers (Select, filtered to exclude already-assigned items) and
 * remove buttons (ConfirmDialog). All mutations go through withSudo.
 *
 * Props:
 *   kind  — 'oidc' | 'saml'   (used to build the base path)
 *   appId — the client ID / provider ID as a string
 *
 * IMPORTANT — busy-guard race prevention:
 *   Three separate useApi() instances are used for the three concurrent on-mount
 *   fetches so that the second/third fetch is never silently short-circuited by
 *   a shared `busy` flag.
 *     accessApi   — GET ${base}/access  + all access mutations (sequential, no race)
 *     groupsApi   — GET /groups  (picker options)
 *     accountsApi — GET /accounts (picker options)
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import SettingRow from '@/components/custom/SettingRow.vue'
import EmptyState from '@/components/custom/EmptyState.vue'

interface AccessGroup {
  id: number
  slug: string
  displayName: string
}

interface AccessAccount {
  id: number
  username: string
  displayName: string
}

interface AccessView {
  accessRestricted: boolean
  groups: AccessGroup[]
  accounts: AccessAccount[]
}

const props = defineProps<{
  kind: 'oidc' | 'saml'
  appId: string
}>()

const { t } = useI18n()

const basePath = computed(() =>
  props.kind === 'oidc'
    ? `/api/prohibitorum/oidc-applications/${props.appId}`
    : `/api/prohibitorum/saml-applications/${props.appId}`
)

// Three SEPARATE useApi() instances — one per concurrent on-mount concern.
// accessApi handles the access view fetch AND all sequential mutations.
const accessApi = useApi()
// groupsApi handles GET /groups (picker options) — must not share accessApi.
const groupsApi = useApi()
// accountsApi handles GET /accounts (picker options) — must not share either above.
const accountsApi = useApi()

const accessRestricted = ref(false)
const assignedGroups = ref<AccessGroup[]>([])
const assignedAccounts = ref<AccessAccount[]>([])
const allGroups = ref<AccessGroup[]>([])
const allAccounts = ref<AccessAccount[]>([])

const selectedGroupId = ref<string>('')
const selectedAccountId = ref<string>('')

const confirmRemoveGroupId = ref<number | null>(null)
const confirmRemoveAccountId = ref<number | null>(null)

const { flag: saved, trigger: triggerSaved } = useTransientFlag()

// Pickers — exclude already-assigned items
const addableGroups = computed(() => {
  const assignedIds = new Set(assignedGroups.value.map((g) => g.id))
  return allGroups.value.filter((g) => !assignedIds.has(g.id))
})
const addableAccounts = computed(() => {
  const assignedIds = new Set(assignedAccounts.value.map((a) => a.id))
  return allAccounts.value.filter((a) => !assignedIds.has(a.id))
})

const groupToRemove = computed(() =>
  assignedGroups.value.find((g) => g.id === confirmRemoveGroupId.value)
)
const accountToRemove = computed(() =>
  assignedAccounts.value.find((a) => a.id === confirmRemoveAccountId.value)
)

async function loadAccess(): Promise<void> {
  const res = await accessApi.run(() => api.get<AccessView>(`${basePath.value}/access`))
  if (res) {
    accessRestricted.value = res.accessRestricted ?? false
    assignedGroups.value = res.groups ?? []
    assignedAccounts.value = res.accounts ?? []
  }
}

async function loadAllGroups(): Promise<void> {
  const res = await groupsApi.run(() => api.get<AccessGroup[]>('/api/prohibitorum/groups'))
  if (Array.isArray(res)) allGroups.value = res
}

async function loadAllAccounts(): Promise<void> {
  const res = await accountsApi.run(() => api.get<AccessAccount[]>('/api/prohibitorum/accounts'))
  if (Array.isArray(res)) allAccounts.value = res
}

async function toggleRestricted(): Promise<void> {
  const next = !accessRestricted.value
  const ok = await accessApi.run(() =>
    withSudo(() => api.post<object>(`${basePath.value}/access/set-restricted`, { restricted: next }))
  )
  if (ok !== undefined) {
    triggerSaved()
    await loadAccess()
  }
}

async function grantGroup(): Promise<void> {
  if (!selectedGroupId.value) return
  const principalId = Number(selectedGroupId.value)
  const ok = await accessApi.run(() =>
    withSudo(() =>
      api.post<object>(`${basePath.value}/access/grant`, { principalKind: 'group', principalId })
    )
  )
  if (ok !== undefined) {
    selectedGroupId.value = ''
    await loadAccess()
  }
}

async function grantAccount(): Promise<void> {
  if (!selectedAccountId.value) return
  const principalId = Number(selectedAccountId.value)
  const ok = await accessApi.run(() =>
    withSudo(() =>
      api.post<object>(`${basePath.value}/access/grant`, { principalKind: 'account', principalId })
    )
  )
  if (ok !== undefined) {
    selectedAccountId.value = ''
    await loadAccess()
  }
}

async function revokeGroup(): Promise<void> {
  if (confirmRemoveGroupId.value === null) return
  const principalId = confirmRemoveGroupId.value
  const ok = await accessApi.run(() =>
    withSudo(() =>
      api.post<object>(`${basePath.value}/access/revoke`, { principalKind: 'group', principalId })
    )
  )
  confirmRemoveGroupId.value = null
  if (ok !== undefined) await loadAccess()
}

async function revokeAccount(): Promise<void> {
  if (confirmRemoveAccountId.value === null) return
  const principalId = confirmRemoveAccountId.value
  const ok = await accessApi.run(() =>
    withSudo(() =>
      api.post<object>(`${basePath.value}/access/revoke`, {
        principalKind: 'account',
        principalId,
      })
    )
  )
  confirmRemoveAccountId.value = null
  if (ok !== undefined) await loadAccess()
}

onMounted(async () => {
  await Promise.all([loadAccess(), loadAllGroups(), loadAllAccounts()])
})
</script>
<template>
  <div class="flex flex-col gap-4">
    <!-- Restrict toggle card -->
    <Card>
      <CardHeader><CardTitle>{{ t('admin.access.title') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-4">
        <Alert v-if="accessApi.errorText.value" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ accessApi.errorText.value }}</AlertDescription>
        </Alert>

        <SettingRow
          :label="t('admin.access.restrictedLabel')"
          :description="t('admin.access.restrictedHint')"
          for="access-restricted"
        >
          <Switch
            id="access-restricted"
            :model-value="accessRestricted"
            :disabled="accessApi.busy.value"
            data-test="access-restricted-toggle"
            @update:model-value="toggleRestricted"
          />
        </SettingRow>

        <StatusMessage :show="saved">{{ t('admin.access.saved') }}</StatusMessage>

        <p v-if="!accessRestricted" class="text-sm text-muted" data-test="access-inactive-hint">
          {{ t('admin.access.inactiveHint') }}
        </p>
      </CardContent>
    </Card>

    <!-- Groups assignment card -->
    <Card>
      <CardHeader><CardTitle>{{ t('admin.access.groups') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-4">
        <Alert v-if="groupsApi.errorText.value" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ groupsApi.errorText.value }}</AlertDescription>
        </Alert>

        <!-- Add group picker -->
        <div class="flex items-center gap-2">
          <Select v-model="selectedGroupId" data-test="group-select">
            <SelectTrigger class="flex-1">
              <SelectValue :placeholder="t('admin.access.addGroup')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="g in addableGroups"
                :key="g.id"
                :value="String(g.id)"
                :data-test="`group-option-${g.id}`"
              >
                {{ g.displayName }}
              </SelectItem>
            </SelectContent>
          </Select>
          <Button
            type="button"
            :disabled="accessApi.busy.value || !selectedGroupId"
            data-test="add-group"
            @click="grantGroup"
          >
            {{ t('admin.access.addGroup') }}
          </Button>
        </div>

        <!-- Assigned groups list -->
        <div v-if="assignedGroups.length" class="flex flex-col gap-2">
          <div
            v-for="g in assignedGroups"
            :key="g.id"
            class="flex items-center justify-between rounded-md border border-border px-3 py-2"
            :data-test="`group-row-${g.id}`"
          >
            <div class="flex min-w-0 flex-col">
              <span class="truncate font-medium text-ink">{{ g.displayName }}</span>
              <span class="truncate font-mono text-xs text-muted">{{ g.slug }}</span>
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              :data-test="`group-remove-${g.id}`"
              @click="confirmRemoveGroupId = g.id"
            >
              {{ t('admin.access.remove') }}
            </Button>
          </div>
        </div>
        <EmptyState v-else :title="t('admin.access.empty')" />
      </CardContent>
    </Card>

    <!-- Accounts assignment card -->
    <Card>
      <CardHeader><CardTitle>{{ t('admin.access.accounts') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-4">
        <Alert v-if="accountsApi.errorText.value" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ accountsApi.errorText.value }}</AlertDescription>
        </Alert>

        <!-- Add account picker -->
        <div class="flex items-center gap-2">
          <Select v-model="selectedAccountId" data-test="account-select">
            <SelectTrigger class="flex-1">
              <SelectValue :placeholder="t('admin.access.addAccount')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="a in addableAccounts"
                :key="a.id"
                :value="String(a.id)"
                :data-test="`account-option-${a.id}`"
              >
                {{ a.displayName }} ({{ a.username }})
              </SelectItem>
            </SelectContent>
          </Select>
          <Button
            type="button"
            :disabled="accessApi.busy.value || !selectedAccountId"
            data-test="add-account"
            @click="grantAccount"
          >
            {{ t('admin.access.addAccount') }}
          </Button>
        </div>

        <!-- Assigned accounts list -->
        <div v-if="assignedAccounts.length" class="flex flex-col gap-2">
          <div
            v-for="a in assignedAccounts"
            :key="a.id"
            class="flex items-center justify-between rounded-md border border-border px-3 py-2"
            :data-test="`account-row-${a.id}`"
          >
            <div class="flex min-w-0 flex-col">
              <span class="truncate font-medium text-ink">{{ a.displayName }}</span>
              <span class="truncate font-mono text-xs text-muted">{{ a.username }}</span>
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              :data-test="`account-remove-${a.id}`"
              @click="confirmRemoveAccountId = a.id"
            >
              {{ t('admin.access.remove') }}
            </Button>
          </div>
        </div>
        <EmptyState v-else :title="t('admin.access.empty')" />
      </CardContent>
    </Card>

    <!-- Remove group confirm -->
    <ConfirmDialog
      :open="confirmRemoveGroupId !== null"
      :title="t('admin.access.removeGroupConfirmTitle')"
      :confirm-label="t('admin.access.remove')"
      :busy="accessApi.busy.value"
      @update:open="(v) => { if (!v) confirmRemoveGroupId = null }"
      @cancel="confirmRemoveGroupId = null"
      @confirm="revokeGroup"
    >
      {{ t('admin.access.removeGroupConfirmBody', { name: groupToRemove?.displayName ?? '' }) }}
    </ConfirmDialog>

    <!-- Remove account confirm -->
    <ConfirmDialog
      :open="confirmRemoveAccountId !== null"
      :title="t('admin.access.removeAccountConfirmTitle')"
      :confirm-label="t('admin.access.remove')"
      :busy="accessApi.busy.value"
      @update:open="(v) => { if (!v) confirmRemoveAccountId = null }"
      @cancel="confirmRemoveAccountId = null"
      @confirm="revokeAccount"
    >
      {{ t('admin.access.removeAccountConfirmBody', { name: accountToRemove?.displayName ?? '' }) }}
    </ConfirmDialog>
  </div>
</template>
