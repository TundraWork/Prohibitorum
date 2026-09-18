<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from 'vue'
import { api } from '@/lib/api'
import type { Page } from '@/lib/pagination'
import { useResource } from '@/composables/useResource'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import EmptyState from '@/components/custom/EmptyState.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import FormSection from '@/components/custom/FormSection.vue'
import PaginationControls from '@/components/custom/PaginationControls.vue'
import UserAvatar from '@/components/custom/UserAvatar.vue'

export interface AccountDirectoryEntry {
  id: number
  username: string
  displayName: string
  disabled: boolean
}

const props = withDefaults(defineProps<{
  excludedAccountIds?: readonly number[]
  busy?: boolean
  title: string
  description: string
  searchLabel: string
  searchPlaceholder: string
  searchAction: string
  searchingLabel: string
  noResultsLabel: string
  searchPrompt: string
  selectLabel: string
  selectAriaLabel: (account: AccountDirectoryEntry) => string
  testPrefix?: string
}>(), {
  excludedAccountIds: () => [],
  busy: false,
  testPrefix: 'account-directory',
})

const emit = defineEmits<{
  (event: 'select', account: AccountDirectoryEntry): void
}>()

const searchQuery = ref('')
const submittedSearch = ref('')
const hasSearched = ref(false)
const cursors = ref([''])
const pageIndex = ref(0)
const searchInputId = `account-directory-search-${useId()}`
const activeCursor = computed(() => cursors.value[pageIndex.value] ?? '')
const accountsApi = useResource(computed(() => ({
  queryKey: ['session', 'accounts', 'directory-picker', submittedSearch.value, activeCursor.value],
  enabled: submittedSearch.value !== '',
  queryFn: ({ signal }: { signal: AbortSignal }) => api.get<Page<AccountDirectoryEntry>>(`/api/prohibitorum/accounts?q=${encodeURIComponent(submittedSearch.value)}&limit=100${activeCursor.value ? `&cursor=${encodeURIComponent(activeCursor.value)}` : ''}`, { signal }),
})))
const nextCursor = computed(() => accountsApi.data.value?.nextCursor ?? '')
const excludedAccountIds = computed(() => new Set(props.excludedAccountIds))
const availableAccounts = computed(() =>
  (accountsApi.data.value?.items ?? []).filter(account =>
    !account.disabled && !excludedAccountIds.value.has(account.id),
  ),
)

function identityName(account: Pick<AccountDirectoryEntry, 'displayName' | 'username'>): string {
  return account.displayName.trim() || account.username
}

async function searchAccounts(): Promise<void> {
  submittedSearch.value = searchQuery.value.trim()
  hasSearched.value = submittedSearch.value !== ''
  cursors.value = ['']
  pageIndex.value = 0
  await nextTick()
}

async function nextPage(): Promise<void> {
  if (accountsApi.busy.value || !nextCursor.value) return
  cursors.value = [...cursors.value.slice(0, pageIndex.value + 1), nextCursor.value]
  pageIndex.value++
  await nextTick()
}

async function previousPage(): Promise<void> {
  if (accountsApi.busy.value || pageIndex.value === 0) return
  pageIndex.value--
  await nextTick()
}

function selectAccount(account: AccountDirectoryEntry): void {
  if (props.busy || accountsApi.busy.value || account.disabled || excludedAccountIds.value.has(account.id)) return
  emit('select', account)
}

watch(searchQuery, query => {
  if (query.trim() !== submittedSearch.value) {
    submittedSearch.value = ''
    hasSearched.value = false
    cursors.value = ['']
    pageIndex.value = 0
  }
})
</script>

<template>
  <FormSection :title="title" :description="description">
    <ErrorPanel :error="accountsApi.error.value" :is-admin="true" @dismiss="accountsApi.clear()" />

    <form role="search" :aria-label="searchLabel" class="flex flex-col gap-2" @submit.prevent="searchAccounts">
      <Label :for="searchInputId">{{ searchLabel }}</Label>
      <div class="flex flex-col gap-2 sm:flex-row">
        <Input
          :id="searchInputId"
          v-model="searchQuery"
          type="search"
          autocomplete="off"
          :spellcheck="false"
          class="flex-1"
          :data-test="`${testPrefix}-search`"
          :aria-label="searchLabel"
          :placeholder="searchPlaceholder"
          @keydown.enter.prevent="searchAccounts"
        />
        <Button type="submit" class="w-full sm:w-auto" :disabled="accountsApi.busy.value">
          {{ searchAction }}
        </Button>
      </div>
    </form>

    <p v-if="accountsApi.busy.value" role="status" class="text-sm text-muted">
      {{ searchingLabel }}
    </p>

    <ul v-else-if="availableAccounts.length" class="flex flex-col gap-2">
      <li
        v-for="account in availableAccounts"
        :key="account.id"
        class="flex flex-col gap-3 rounded-lg border border-border bg-sunken px-4 py-3 sm:flex-row sm:items-center sm:justify-between"
        :data-test="`${testPrefix}-result-${account.id}`"
      >
        <div class="flex min-w-0 items-start gap-3">
          <UserAvatar :display-name="account.displayName" :username="account.username" />
          <div class="min-w-0">
            <p class="truncate font-medium text-ink">{{ identityName(account) }}</p>
            <p class="truncate font-mono text-xs text-muted">{{ account.username }}</p>
          </div>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          class="w-full sm:w-auto"
          :disabled="busy || accountsApi.busy.value"
          :aria-label="selectAriaLabel(account)"
          :data-test="`${testPrefix}-select-${account.id}`"
          @click="selectAccount(account)"
        >
          {{ selectLabel }}
        </Button>
      </li>
    </ul>

    <EmptyState v-else-if="hasSearched && !accountsApi.error.value" :title="noResultsLabel" />

    <p v-else-if="!accountsApi.error.value" class="text-sm text-muted">
      {{ searchPrompt }}
    </p>

    <PaginationControls
      v-if="hasSearched"
      :page-index="pageIndex"
      :has-more="!!nextCursor"
      :busy="accountsApi.busy.value"
      :has-items="availableAccounts.length > 0"
      @next="nextPage"
      @previous="previousPage"
    />
  </FormSection>
</template>
