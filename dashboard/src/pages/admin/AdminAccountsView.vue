<script setup lang="ts">
/** AdminAccountsView (/admin/accounts) — server-filtered, cursor-paginated directory. */
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useCursorPage } from '@/composables/useCursorPage'
import { type Page, buildPagePath } from '@/lib/pagination'
import { relativeTime } from '@/lib/time'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import UserAvatar from '@/components/custom/UserAvatar.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import IdentityMetadata, { type AccountIdentity } from '@/components/custom/IdentityMetadata.vue'
import PaginationControls from '@/components/custom/PaginationControls.vue'
import { Users } from 'lucide-vue-next'

interface Account {
  id: number
  username: string
  displayName: string
  role: string
  disabled: boolean
  lastSignInAt?: string
  avatarUrl?: string
  matchingIdentities: AccountIdentity[]
}

type MatchOperator = 'exact' | 'prefix' | 'contains'

interface IdentitySearchField {
  key: string
  operators: MatchOperator[]
}

interface IdentityProviderDescriptor {
  slug: string
  displayName: string
  protocol: string
  searchFields: IdentitySearchField[]
}

const ALL_PROVIDERS = '__all__'
const FIELD_LABEL_KEYS: Record<string, string> = {
  subject: 'identity.subject',
  email: 'identity.email',
  steamId: 'identity.steamId',
  personaName: 'identity.personaName',
  userId: 'identity.vrchatUserId',
  displayName: 'identity.displayName',
}
const MATCH_LABEL_KEYS: Record<MatchOperator, string> = {
  exact: 'admin.accounts.matchExact',
  prefix: 'admin.accounts.matchPrefix',
  contains: 'admin.accounts.matchContains',
}

const { t } = useI18n()
const router = useRouter()
const providersApi = useApi()
const descriptors = ref<IdentityProviderDescriptor[]>([])
const searchDraft = ref('')
const q = ref('')
const providerSlug = ref(ALL_PROVIDERS)
const fieldKey = ref('')
const matchOperator = ref('')
const filterValue = ref('')
let searchTimer: ReturnType<typeof setTimeout> | undefined

const selectedDescriptor = computed(() =>
  descriptors.value.find((descriptor) => descriptor.slug === providerSlug.value),
)
const descriptorFields = computed(() =>
  (selectedDescriptor.value?.searchFields ?? []).filter((field) => FIELD_LABEL_KEYS[field.key]),
)
const selectedField = computed(() =>
  descriptorFields.value.find((field) => field.key === fieldKey.value),
)

const effectiveAdvanced = computed<Record<string, string>>(() => {
  if (providerSlug.value === ALL_PROVIDERS) return {}
  const filters: Record<string, string> = { provider: providerSlug.value }
  const value = filterValue.value.trim()
  const operator = matchOperator.value as MatchOperator
  if (selectedField.value && selectedField.value.operators.includes(operator) && value) {
    filters.field = selectedField.value.key
    filters.value = value
    filters.match = operator
  }
  return filters
})

const filterSignature = computed(() => JSON.stringify({ q: q.value, ...effectiveAdvanced.value }))
const hasActiveFilters = computed(() => q.value !== '' || providerSlug.value !== ALL_PROVIDERS)
const hasFilterDraft = computed(() =>
  searchDraft.value !== ''
  || providerSlug.value !== ALL_PROVIDERS
  || fieldKey.value !== ''
  || matchOperator.value !== ''
  || filterValue.value !== '',
)

const page = useCursorPage<Account>((cursor) => {
  const params: Record<string, string> = {}
  if (q.value) params.q = q.value
  Object.assign(params, effectiveAdvanced.value)
  if (cursor) params.cursor = cursor
  return api.get<Page<Account>>(buildPagePath('/api/prohibitorum/accounts', params))
})
const rows = page.items
const displayError = computed(() => page.error.value ?? providersApi.error.value)
let appliedSignature = filterSignature.value

function resetForFilters(): void {
  const signature = filterSignature.value
  if (signature === appliedSignature) return
  appliedSignature = signature
  void page.reset()
}

watch(searchDraft, (draft) => {
  if (searchTimer !== undefined) clearTimeout(searchTimer)
  searchTimer = undefined
  const normalized = draft.trim()
  if (normalized === q.value) return
  searchTimer = setTimeout(() => {
    searchTimer = undefined
    q.value = normalized
    resetForFilters()
  }, 300)
})

watch(effectiveAdvanced, resetForFilters)

function setProvider(value: unknown): void {
  if (typeof value !== 'string') return
  if (value !== ALL_PROVIDERS && !descriptors.value.some((descriptor) => descriptor.slug === value)) return
  providerSlug.value = value
  fieldKey.value = ''
  matchOperator.value = ''
  filterValue.value = ''
}

function setField(value: unknown): void {
  if (typeof value !== 'string' || !descriptorFields.value.some((field) => field.key === value)) return
  fieldKey.value = value
  matchOperator.value = ''
  filterValue.value = ''
}

function setMatch(value: unknown): void {
  if (typeof value !== 'string' || !selectedField.value?.operators.includes(value as MatchOperator)) return
  matchOperator.value = value
}

function clearFilters(): void {
  if (searchTimer !== undefined) clearTimeout(searchTimer)
  searchTimer = undefined
  searchDraft.value = ''
  q.value = ''
  providerSlug.value = ALL_PROVIDERS
  fieldKey.value = ''
  matchOperator.value = ''
  filterValue.value = ''
  resetForFilters()
}

function clearError(): void {
  page.clear()
  providersApi.clear()
}

function fieldLabel(key: string): string {
  return t(FIELD_LABEL_KEYS[key] ?? 'identity.subject')
}

function matchLabel(operator: MatchOperator): string {
  return t(MATCH_LABEL_KEYS[operator])
}

async function loadDescriptors(): Promise<void> {
  const result = await providersApi.run(() =>
    api.get<Page<IdentityProviderDescriptor>>(
      buildPagePath('/api/prohibitorum/identity-providers', { limit: 100 }),
    ),
  )
  if (result) descriptors.value = result.items ?? []
}

function go(id: number): void {
  router.push(`/admin/accounts/${id}`)
}

onMounted(() => { void loadDescriptors() })
onBeforeUnmount(() => {
  if (searchTimer !== undefined) clearTimeout(searchTimer)
})
</script>

<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.accounts.title') }}</h1>
      <Button type="button" data-test="invite" @click="router.push('/admin/invitations')">{{ t('admin.accounts.invite') }}</Button>
    </div>

    <ErrorPanel :error="displayError" @dismiss="clearError" :is-admin="true" />

    <div role="search" :aria-label="t('admin.accounts.advancedFilters')" class="flex min-w-0 flex-col gap-3">
      <Input
        v-model="searchDraft"
        type="search"
        data-test="accounts-filter"
        :aria-label="t('admin.accounts.filterPlaceholder')"
        :placeholder="t('admin.accounts.filterPlaceholder')"
        class="w-full sm:max-w-md"
      />

      <fieldset class="min-w-0">
        <legend class="sr-only">{{ t('admin.accounts.advancedFilters') }}</legend>
        <div class="flex min-w-0 flex-wrap items-end gap-3 rounded-md border border-border bg-sunken p-3">
          <div class="flex min-w-0 flex-1 basis-32 flex-col gap-1.5">
            <Label for="accounts-provider">{{ t('admin.accounts.provider') }}</Label>
            <Select :model-value="providerSlug" @update:model-value="setProvider">
              <SelectTrigger id="accounts-provider" data-test="accounts-provider" class="w-full">
                <SelectValue :placeholder="t('admin.accounts.allProviders')" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem :value="ALL_PROVIDERS">{{ t('admin.accounts.allProviders') }}</SelectItem>
                <SelectItem v-for="descriptor in descriptors" :key="descriptor.slug" :value="descriptor.slug">
                  {{ descriptor.displayName }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div v-if="selectedDescriptor" class="flex min-w-0 flex-1 basis-32 flex-col gap-1.5">
            <Label for="accounts-field">{{ t('admin.accounts.field') }}</Label>
            <Select :model-value="fieldKey || undefined" @update:model-value="setField">
              <SelectTrigger id="accounts-field" data-test="accounts-field" class="w-full">
                <SelectValue :placeholder="t('admin.accounts.chooseField')" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="field in descriptorFields" :key="field.key" :value="field.key">
                  {{ fieldLabel(field.key) }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div v-if="selectedField" class="flex min-w-0 flex-1 basis-32 flex-col gap-1.5">
            <Label for="accounts-match">{{ t('admin.accounts.match') }}</Label>
            <Select :model-value="matchOperator || undefined" @update:model-value="setMatch">
              <SelectTrigger id="accounts-match" data-test="accounts-match" class="w-full">
                <SelectValue :placeholder="t('admin.accounts.chooseMatch')" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="operator in selectedField.operators" :key="operator" :value="operator">
                  {{ matchLabel(operator) }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div v-if="selectedField" class="flex min-w-0 flex-1 basis-32 flex-col gap-1.5">
            <Label for="accounts-advanced-value">{{ t('admin.accounts.value') }}</Label>
            <Input
              id="accounts-advanced-value"
              v-model="filterValue"
              data-test="accounts-advanced-value"
              :placeholder="t('admin.accounts.valuePlaceholder')"
            />
          </div>

          <Button
            v-if="hasFilterDraft"
            type="button"
            variant="outline"
            class="w-full shrink-0 sm:w-auto"
            data-test="accounts-clear"
            @click="clearFilters"
          >{{ t('admin.accounts.clearFilters') }}</Button>
        </div>
      </fieldset>
    </div>

    <TableSkeleton v-if="page.busy.value && !rows.length" :rows="5" :cols="4" />
    <Table v-else-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.accounts.colUser') }} · {{ t('admin.accounts.colUsername') }}</TableHead>
          <TableHead>{{ t('admin.accounts.colRole') }}</TableHead>
          <TableHead>{{ t('admin.accounts.colState') }}</TableHead>
          <TableHead>{{ t('admin.accounts.colLastSeen') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="account in rows"
          :key="account.id"
          class="cursor-pointer"
          tabindex="0"
          :data-test="`account-row-${account.id}`"
          @click="go(account.id)"
          @keydown.enter="go(account.id)"
          @keydown.space.prevent="go(account.id)"
        >
          <TableCell>
            <div class="flex min-w-0 items-start gap-2">
              <UserAvatar :display-name="account.displayName" :username="account.username" :src="account.avatarUrl" size="sm" />
              <div class="flex min-w-0 flex-1 flex-col">
                <span class="truncate font-medium text-ink">{{ account.displayName }}</span>
                <span class="truncate text-muted">@{{ account.username }}</span>
                <div v-if="account.matchingIdentities?.length" class="mt-2 flex min-w-0 flex-col gap-2 border-t border-border pt-2">
                  <p class="text-xs font-medium text-ink">{{ t('admin.accounts.matchingIdentity') }}</p>
                  <div
                    v-for="identity in account.matchingIdentities"
                    :key="identity.id"
                    class="min-w-0 rounded-md bg-sunken px-3 py-2"
                  >
                    <p class="mb-1 truncate text-sm font-medium text-ink" :title="identity.providerDisplayName">
                      {{ identity.providerDisplayName }}
                    </p>
                    <IdentityMetadata :identity="identity" />
                  </div>
                </div>
              </div>
            </div>
          </TableCell>
          <TableCell>
            <StatusBadge :variant="account.role === 'admin' ? 'caution' : 'neutral'">
              {{ account.role === 'admin' ? t('admin.account.roleAdmin') : t('admin.account.roleUser') }}
            </StatusBadge>
          </TableCell>
          <TableCell>
            <StatusBadge :variant="account.disabled ? 'danger' : 'success'">
              {{ account.disabled ? t('admin.accounts.disabled') : t('admin.accounts.active') }}
            </StatusBadge>
          </TableCell>
          <TableCell class="text-muted">{{ relativeTime(account.lastSignInAt) }}</TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <p v-else-if="hasActiveFilters && !page.error.value" role="status" class="text-sm text-muted" data-test="accounts-no-matches">
      {{ t('admin.accounts.noMatches') }}
    </p>
    <EmptyState v-else-if="!page.error.value" :icon="Users" :title="t('admin.accounts.empty')" />

    <PaginationControls
      v-if="rows.length || page.pageIndex.value > 0"
      :page-index="page.pageIndex.value"
      :has-more="page.hasMore.value"
      :busy="page.busy.value"
      :has-items="rows.length > 0"
      @next="page.next"
      @previous="page.previous"
    />
  </div>
</template>
